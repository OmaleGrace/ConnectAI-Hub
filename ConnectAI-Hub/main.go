package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

	// Stop cleanly on Ctrl+C or a deploy: finish requests that are in progress
	// (an AI answer can take a while) instead of cutting them off.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app.StartJanitor(ctx)

	server := &http.Server{Addr: addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() {
		log.Printf("ConnectAI Hub running at http://localhost%s", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown: %v", err)
	}
}
