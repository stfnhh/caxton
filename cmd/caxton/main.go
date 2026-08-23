package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"caxton/internal/library"
	"caxton/internal/server"
)

func main() {
	var (
		dir      = flag.String("library", env("CAXTON_LIBRARY", "./library"), "directory containing EPUB and M4B files")
		addr     = flag.String("listen", env("CAXTON_LISTEN", ":8080"), "HTTP listen address")
		interval = flag.Duration("scan-interval", envDuration("CAXTON_SCAN_INTERVAL", 5*time.Minute), "library rescan interval")
		baseURL  = flag.String("base-url", os.Getenv("CAXTON_BASE_URL"), "public base URL (otherwise inferred from each request)")
		database = flag.String("database", env("CAXTON_DATABASE", "./caxton.db"), "SQLite metadata database")
		covers   = flag.String("cover-cache", env("CAXTON_COVER_CACHE", "./covers"), "directory for extracted cover images")
	)
	flag.Parse()

	log.Printf("scanning %s", *dir)
	catalog, err := library.NewCatalog(*dir, *database, *covers)
	if err != nil {
		log.Fatal(err)
	}
	defer catalog.Close()
	if err := catalog.Scan(); err != nil {
		log.Printf("initial scan: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go catalog.Run(ctx, *interval, func(err error) { log.Printf("scan: %v", err) })

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server.New(catalog, *baseURL),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("Caxton listening on %s", *addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		log.Fatalf("invalid %s: %v", key, err)
	}
	return d
}
