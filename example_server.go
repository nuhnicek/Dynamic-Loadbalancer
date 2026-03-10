package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

func main() {
	balancerConn := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	go func() {
		for {
			fmt.Println("reactivated backend")
			err := nodeActivation(balancerConn, "http://localhost:8082")
			if err != nil {
				fmt.Println("Error activating node:", err)
			}
			time.Sleep(8 * time.Second)
		}
	}()
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "cusne")
	})
	println("starting server")
	panic(http.ListenAndServe(":8082", nil))

}

func nodeActivation(client *redis.Client, node string) error {
	err := client.Set(context.Background(), "server:"+node, "up", 10*time.Second).Err()
	if err != nil {
		return err
	}
	return nil
}
