package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"example.com/stream-signup-worker/internal/httpapi"
	infraiclient "example.com/stream-signup-worker/internal/infrai"
	"example.com/stream-signup-worker/internal/media"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}

	queue := media.NewQueue(64)
	server := httpapi.New(infraiclient.New(apiKey, nil), queue)
	go func() {
		for {
			if _, ok := queue.ProcessNext(); !ok {
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	addr := ":8080"
	log.Printf("media-sessiond listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}
