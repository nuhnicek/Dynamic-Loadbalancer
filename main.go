package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Backend holds the connection info
type Backend struct {
	ID    string
	URL   *url.URL
	Proxy *httputil.ReverseProxy
}

type LoadBalancer struct {
	backends  []*Backend
	serverMap map[string]*Backend
	counter   uint64
	mu        sync.RWMutex
	rdb       *redis.Client
}

func NewLoadBalancer(redisAddr string) *LoadBalancer {
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	return &LoadBalancer{
		serverMap: make(map[string]*Backend),
		rdb:       rdb,
	}
}

// syncBackends compares Redis state with internal state
func (lb *LoadBalancer) syncBackends() {
	ctx := context.Background()
	// 1. Get all active heartbeats from Redis
	keys, err := lb.rdb.Keys(ctx, "server:*").Result()
	if err != nil {
		fmt.Println("Redis error:", err)
		return
	}

	newMap := make(map[string]*Backend)
	var newList []*Backend

	for _, key := range keys {
		// Key format is "server:http://localhost:8081"
		serverURL := strings.TrimPrefix(key, "server:")
		target, _ := url.Parse(serverURL)

		id := fmt.Sprintf("%x", md5.Sum([]byte(serverURL)))[:8]

		backend := &Backend{
			ID:    id,
			URL:   target,
			Proxy: httputil.NewSingleHostReverseProxy(target),
		}
		newMap[id] = backend
		newList = append(newList, backend)
	}

	// 2. Atomic update of the server list
	lb.mu.Lock()
	lb.backends = newList
	lb.serverMap = newMap
	lb.mu.Unlock()

	fmt.Printf("[Sync] Active nodes: %d\n", len(newList))
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lb.mu.RLock()
	if len(lb.backends) == 0 {
		lb.mu.RUnlock()
		http.Error(w, "No backends available", http.StatusServiceUnavailable)
		return
	}

	var target *Backend
	cookie, err := r.Cookie("server_id")

	// Sticky Logic
	if err == nil {
		if b, ok := lb.serverMap[cookie.Value]; ok {
			target = b
		}
	}

	// Round Robin Fallback
	if target == nil {
		n := atomic.AddUint64(&lb.counter, 1)
		target = lb.backends[n%uint64(len(lb.backends))]

		http.SetCookie(w, &http.Cookie{
			Name: "server_id", Value: target.ID, Path: "/", HttpOnly: true,
		})
	}
	lb.mu.RUnlock()

	target.Proxy.ServeHTTP(w, r)
}

func main() {
	lb := NewLoadBalancer("localhost:6379")

	// Background: Keep the server list fresh from Redis
	go func() {
		for {
			lb.syncBackends()
			time.Sleep(5 * time.Second)
		}
	}()

	fmt.Println("Redis-Discovery Load Balancer on :8080")
	panic(http.ListenAndServe(":8080", lb))
}
