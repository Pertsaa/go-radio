package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Pertsaa/go-radio/internal/handler"
	"github.com/Pertsaa/go-radio/internal/middleware"
)

func main() {
	ctx := context.Background()

	r := http.NewServeMux()

	h := handler.NewAppHandler(ctx)

	r.HandleFunc("GET /", handler.Make(h.IndexHandler))
	r.HandleFunc("GET /index.css", handler.Make(h.CSSHandler))
	r.HandleFunc("GET /favicon.png", handler.Make(h.FaviconHandler))

	stack := middleware.CreateStack(
		middleware.Log,
		middleware.CORS,
	)

	server := http.Server{
		Addr:    ":3000",
		Handler: stack(r),
	}

	fmt.Println("Server listening on port 3000...")
	if err := server.ListenAndServe(); err != nil {
		fmt.Println("Error starting server:", err)
	}
}
