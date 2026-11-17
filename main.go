package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"connectai-hub/internal/database"
	"connectai-hub/internal/web"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	addr := os.Getenv("APP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	db, err := database.Open(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("database connection: %v", err)
	}
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		log.Fatalf("database migration: %v", err)
	}

	app, err := web.New(db)
	if err != nil {
		log.Fatalf("initialise web application: %v", err)
	}

	log.Printf("ConnectAI Hub running at http://localhost%s", addr)
	server := &http.Server{Addr: addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.ListenAndServe())
}
