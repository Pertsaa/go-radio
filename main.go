package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/Pertsaa/go-radio/handler"
	"github.com/Pertsaa/go-radio/middleware"
	"github.com/Pertsaa/go-radio/radio"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: ./go-radio <data_dir>")
		os.Exit(1)
	}

	dataDir := os.Args[1]

	goRadio := radio.New(dataDir)

	err := goRadio.LoadChannels()
	if err != nil {
		log.Fatalf("Server failed to load channels: %v", err)
	}

	go goRadio.Broadcast()

	ctx := context.Background()

	r := http.NewServeMux()

	h := handler.NewHandler(ctx, goRadio)

	r.HandleFunc("GET /", handler.Make(h.IndexHandler))
	r.HandleFunc("GET /index.css", handler.Make(h.CSSHandler))
	r.HandleFunc("GET /favicon.png", handler.Make(h.FaviconHandler))

	r.HandleFunc("GET /api/radio/channels", handler.Make(h.RadioChannelListHandler))
	r.HandleFunc("GET /api/radio/{channelID}/stream", handler.Make(h.RadioChannelStreamHandler))

	stack := middleware.CreateStack(
		middleware.Log,
		middleware.CORS,
	)

	server := http.Server{
		Addr:    ":8080",
		Handler: stack(r),
	}

	fmt.Println("Server listening on port 8080...")
	if err := server.ListenAndServe(); err != nil {
		fmt.Println("Error starting server:", err)
	}
}
