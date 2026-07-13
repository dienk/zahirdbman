// Command server starts the zahirdbman web application.
package main

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/zahir/zahirdbman/internal/profile"
	"github.com/zahir/zahirdbman/internal/store"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-version", "--version", "-v":
			fmt.Println("zahirdbman", version)
			return
		}
	}

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("zahirdbman ")
	log.Printf("zahirdbman %s starting", version)

	cfg := config.Load()

	// Load connection profiles. If none exist yet, seed one from the environment
	// so the app has a working default connection out of the box.
	profiles, err := profile.Load(cfg.ProfilesFile)
	if err != nil {
		log.Fatalf("load profiles: %v", err)
	}
	if profiles.Empty() {
		seed := profile.Profile{
			Name: "Default (env)", Host: cfg.PGHost, Port: cfg.PGPort,
			User: cfg.PGUser, Password: cfg.PGPassword, SSLMode: cfg.PGSSLMode, AdminDB: cfg.AdminDatabase,
		}
		if err := profiles.Upsert(seed); err != nil {
			log.Fatalf("seed default profile: %v", err)
		}
		log.Printf("seeded default connection profile at %s", cfg.ProfilesFile)
	}

	// Start with the active profile's connection.
	if active, ok := profiles.Active(); ok {
		cfg = cfg.WithConn(active.Host, active.Port, active.User, active.Password, active.SSLMode, active.AdminDB)
		log.Printf("active connection profile: %q (%s:%s)", active.Name, active.Host, active.Port)
	}

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

	h, err := handler.New(mgr, tools, profiles, cfg, zahirdbman.WebFS)
	if err != nil {
		log.Fatalf("init handler: %v", err)
	}

	if cfg.CORSOrigin != "" {
		log.Printf("JSON API CORS enabled for origin %q", cfg.CORSOrigin)
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           logRequests(corsMiddleware(cfg.CORSOrigin, h.Routes())),
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

// corsMiddleware adds CORS headers for the configured origin so a browser
// frontend (e.g. on Vercel) can call the JSON API. When origin is empty it is a
// no-op, so cross-origin API access stays disabled by default.
func corsMiddleware(origin string, next http.Handler) http.Handler {
	if origin == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			// Private Network Access: allow a public HTTPS page (e.g. Vercel) to
			// reach this backend when it is on localhost / a private network.
			if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
				w.Header().Set("Access-Control-Allow-Private-Network", "true")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logRequests is a minimal access-log middleware.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
