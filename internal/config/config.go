// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
)

// Config holds the server and default PostgreSQL connection settings.
type Config struct {
	// HTTP server address, e.g. ":8080".
	Addr string

	// PostgreSQL connection parameters. These describe how zahirdbman reaches
	// the server; individual databases are selected per-request.
	PGHost     string
	PGPort     string
	PGUser     string
	PGPassword string
	PGSSLMode  string

	// AdminDatabase is the maintenance database used for server-wide queries
	// such as listing or creating databases (defaults to "postgres").
	AdminDatabase string
}

// Load reads configuration from the environment, applying sensible defaults.
func Load() Config {
	return Config{
		Addr:          env("ZDBM_ADDR", ":8080"),
		PGHost:        env("PGHOST", "localhost"),
		PGPort:        env("PGPORT", "5432"),
		PGUser:        env("PGUSER", "postgres"),
		PGPassword:    env("PGPASSWORD", ""),
		PGSSLMode:     env("PGSSLMODE", "prefer"),
		AdminDatabase: env("ZDBM_ADMIN_DB", "postgres"),
	}
}

// DSN builds a libpq-style connection string for the given database name.
func (c Config) DSN(database string) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.PGUser, c.PGPassword),
		Host:   fmt.Sprintf("%s:%s", c.PGHost, c.PGPort),
		Path:   "/" + database,
	}
	q := u.Query()
	q.Set("sslmode", c.PGSSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
