// Command ledgerd is the entry point for the Money Kernel HTTP server.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/gubaros/del-e-very/core-ledger/internal/adapters/postgres"
	"github.com/gubaros/del-e-very/core-ledger/internal/application"
	httpapi "github.com/gubaros/del-e-very/core-ledger/internal/api/http"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("ledgerd: %v", err)
	}
}

func run() error {
	dsn := mustEnv("DATABASE_URL")
	port := envOr("PORT", "8080")
	addr := ":" + port

	// --- Database ---
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging database: %w", err)
	}

	// --- Migrations ---
	log.Println("running migrations…")
	if err := postgres.Migrate(db); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	log.Println("migrations complete")

	// --- Wire ---
	store := postgres.NewStore(db)
	svc := application.NewLedgerService(store, application.RealClock{})
	handler := httpapi.NewHandler(svc)
	router := httpapi.NewRouter(handler)

	// --- HTTP Server ---
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("ledgerd listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down…")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	return srv.Shutdown(shutCtx)
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
