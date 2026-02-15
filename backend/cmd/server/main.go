package main

import (
	"log"
	"net/http"
	"os"

	"github.com/bits-assignment/dating-platform/backend/internal/config"
	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/router"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	if err := db.Init(cfg.DatabaseURL); err != nil {
		log.Fatalf("database init: %v", err)
	}
	defer db.Close()

	r := router.New(cfg)
	addr := ":" + cfg.Port
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}
	log.Printf("server listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("server: %v", err)
	}
}
