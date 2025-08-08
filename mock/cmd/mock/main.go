package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Order struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

type Product struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func main() {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Simple mock API under /api
	r.Get("/api/orders/{orderId}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "orderId")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Order{ID: id, Amount: 100})
	})

	r.Get("/api/products/{productId}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "productId")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Product{ID: id, Name: "Sample"})
	})

	addr := ":9090"
	log.Printf("Mock REST server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
