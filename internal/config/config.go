// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

	// ProfilesFile is where connection profiles are persisted as JSON.
	ProfilesFile string

	// CORSOrigin, when set, enables the JSON API for that browser origin
	// (e.g. a Vercel frontend URL). Empty disables cross-origin API access.
	CORSOrigin string
}

// WithConn returns a copy of c whose PostgreSQL connection fields are replaced.
// The HTTP server settings (Addr, ProfilesFile) are preserved. Empty values
// fall back to the receiver's current value.
func (c Config) WithConn(host, port, user, password, sslmode, adminDB string) Config {
	out := c
	if host != "" {
		out.PGHost = host
	}
	if port != "" {
		out.PGPort = port
	}
	out.PGUser = user
	out.PGPassword = password
	if sslmode != "" {
		out.PGSSLMode = sslmode
	}
	if adminDB != "" {
		out.AdminDatabase = adminDB
	}
	return out
}

// Load reads configuration from the environment, applying sensible defaults.
//
// Resolution order for the connection: built-in defaults, then a DATABASE_URL
// connection string if present (as provided by PaaS platforms like Render),
// then explicit PG* variables, which win. The listen address prefers
// ZDBM_ADDR, then a PaaS-provided PORT, then :8080.
func Load() Config {
	c := Config{
		Addr:          resolveAddr(),
		PGHost:        "localhost",
		PGPort:        "5432",
		PGUser:        "postgres",
		PGPassword:    "",
		PGSSLMode:     "prefer",
		AdminDatabase: "postgres",
		ProfilesFile:  env("ZDBM_PROFILES", defaultProfilesPath()),
		CORSOrigin:    env("ZDBM_CORS_ORIGIN", ""),
	}
	if raw := firstNonEmpty(os.Getenv("DATABASE_URL"), os.Getenv("ZDBM_DATABASE_URL")); raw != "" {
		applyDatabaseURL(&c, raw)
	}
	// Explicit PG* variables override the URL and the defaults.
	c.PGHost = env("PGHOST", c.PGHost)
	c.PGPort = env("PGPORT", c.PGPort)
	c.PGUser = env("PGUSER", c.PGUser)
	c.PGPassword = env("PGPASSWORD", c.PGPassword)
	c.PGSSLMode = env("PGSSLMODE", c.PGSSLMode)
	c.AdminDatabase = env("ZDBM_ADMIN_DB", c.AdminDatabase)
	return c
}

// resolveAddr picks the HTTP listen address: ZDBM_ADDR, else :$PORT (PaaS
// convention), else :8080.
func resolveAddr() string {
	if a := os.Getenv("ZDBM_ADDR"); a != "" {
		return a
	}
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return ":8080"
}

// applyDatabaseURL fills connection fields from a postgres:// URL.
func applyDatabaseURL(c *Config, raw string) {
	u, err := url.Parse(raw)
	if err != nil {
		return
	}
	if h := u.Hostname(); h != "" {
		c.PGHost = h
	}
	if p := u.Port(); p != "" {
		c.PGPort = p
	}
	if u.User != nil {
		if un := u.User.Username(); un != "" {
			c.PGUser = un
		}
		if pw, ok := u.User.Password(); ok {
			c.PGPassword = pw
		}
	}
	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		c.AdminDatabase = db
	}
	if sm := u.Query().Get("sslmode"); sm != "" {
		c.PGSSLMode = sm
	}
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// defaultProfilesPath returns the OS config-dir location for the profiles file,
// falling back to the working directory if that cannot be determined.
func defaultProfilesPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return "zahirdbman-profiles.json"
	}
	return filepath.Join(dir, "zahirdbman", "profiles.json")
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
