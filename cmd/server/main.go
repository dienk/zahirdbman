// Command server starts the zahirdbman web application.
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

	zahirdbman "github.com/zahir/zahirdbman"
	"github.com/zahir/zahirdbman/internal/backup"
	"github.com/zahir/zahirdbman/internal/config"
	"github.com/zahir/zahirdbman/internal/handler"
	"github.com/zahir/zahirdbman/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("zahirdbman ")

	cfg := config.Load()
	mgr := store.New(cfg)
	defer mgr.Close()

	// Verify connectivity at startup, but don't hard-fail: the DB may come up
	// after the web server. Log clearly either way.
	startupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := mgr.Ping(startupCtx); err != nil {
		log.Printf("warning: could not reach PostgreSQL at %s:%s (%v)", cfg.PGHost, cfg.PGPort, err)
	} else {
		log.Printf("connected to PostgreSQL at %s:%s", cfg.PGHost, cfg.PGPort)
	}
	cancel()

	tools := backup.New(cfg)
	if tools.Available() {
		log.Println("backup/restore enabled (pg_dump, pg_restore, psql found)")
	} else {
		log.Printf("backup/restore limited: missing client tools %v", tools.Missing())
	}

	h, err := handler.New(mgr, tools, zahirdbman.WebFS)
	if err != nil {
		log.Fatalf("init handler: %v", err)
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           logRequests(h.Routes()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on http://localhost%s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

// logRequests is a minimal access-log middleware.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
