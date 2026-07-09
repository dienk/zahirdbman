// Package store provides a thin, safe access layer over PostgreSQL used by
// zahirdbman to inspect and manage databases, schemas, tables and rows.
package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zahir/zahirdbman/internal/config"
)

// Manager owns a cache of connection pools, one per target database, all
// sharing the same server credentials from config.
type Manager struct {
	cfg   config.Config
	mu    sync.Mutex
	pools map[string]*pgxpool.Pool
}

// New returns a Manager for the given configuration.
func New(cfg config.Config) *Manager {
	return &Manager{cfg: cfg, pools: make(map[string]*pgxpool.Pool)}
}

// Test opens a short-lived connection using cfg and returns the server version,
// without touching the Manager's active connection. Used to validate a profile.
func Test(ctx context.Context, cfg config.Config) (string, error) {
	p, err := pgxpool.New(ctx, cfg.DSN(cfg.AdminDatabase))
	if err != nil {
		return "", err
	}
	defer p.Close()
	var v string
	if err := p.QueryRow(ctx, "SELECT version()").Scan(&v); err != nil {
		return "", err
	}
	return v, nil
}

// ConnInfo describes the currently active connection for display.
type ConnInfo struct {
	Host    string
	Port    string
	User    string
	AdminDB string
	SSLMode string
}

// ConnInfo returns the active connection parameters (without the password).
func (m *Manager) ConnInfo() ConnInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return ConnInfo{
		Host:    m.cfg.PGHost,
		Port:    m.cfg.PGPort,
		User:    m.cfg.PGUser,
		AdminDB: m.cfg.AdminDatabase,
		SSLMode: m.cfg.PGSSLMode,
	}
}

// admin returns the active admin database name under lock.
func (m *Manager) admin() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.AdminDatabase
}

// Reconfigure switches the active connection: every cached pool is closed and
// the new connection parameters take effect for subsequent operations.
func (m *Manager) Reconfigure(cfg config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pools {
		p.Close()
	}
	m.pools = make(map[string]*pgxpool.Pool)
	m.cfg = cfg
}

// pool returns (creating if needed) a pool connected to the named database.
func (m *Manager) pool(ctx context.Context, database string) (*pgxpool.Pool, error) {
	if database == "" {
		database = m.cfg.AdminDatabase
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.pools[database]; ok {
		return p, nil
	}
	p, err := pgxpool.New(ctx, m.cfg.DSN(database))
	if err != nil {
		return nil, fmt.Errorf("connect to %q: %w", database, err)
	}
	m.pools[database] = p
	return p, nil
}

// Close releases every cached pool.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pools {
		p.Close()
	}
	m.pools = make(map[string]*pgxpool.Pool)
}

// Ping verifies the server is reachable via the admin database.
func (m *Manager) Ping(ctx context.Context) error {
	p, err := m.pool(ctx, m.admin())
	if err != nil {
		return err
	}
	return p.Ping(ctx)
}

// ServerVersion returns the PostgreSQL version string.
func (m *Manager) ServerVersion(ctx context.Context) (string, error) {
	p, err := m.pool(ctx, m.admin())
	if err != nil {
		return "", err
	}
	var v string
	err = p.QueryRow(ctx, "SELECT version()").Scan(&v)
	return v, err
}

// ServerInfo holds descriptive facts about the connected server.
type ServerInfo struct {
	Full      string    // the raw version() string
	Version   string    // short version, e.g. "14.20"
	StartedAt time.Time // postmaster start time
}

// ServerInfo returns version and uptime details for the connected server.
func (m *Manager) ServerInfo(ctx context.Context) (ServerInfo, error) {
	p, err := m.pool(ctx, m.admin())
	if err != nil {
		return ServerInfo{}, err
	}
	var info ServerInfo
	err = p.QueryRow(ctx, "SELECT version(), pg_postmaster_start_time()").
		Scan(&info.Full, &info.StartedAt)
	if err != nil {
		return ServerInfo{}, err
	}
	// version() looks like "PostgreSQL 14.20 (Homebrew) on ...".
	if f := strings.Fields(info.Full); len(f) >= 2 && f[0] == "PostgreSQL" {
		info.Version = f[1]
	}
	return info, nil
}

// Database describes a single database and its size.
type Database struct {
	Name    string
	Owner   string
	Size    string
	SizeRaw int64
}

// ListDatabases returns non-template databases ordered by name.
func (m *Manager) ListDatabases(ctx context.Context) ([]Database, error) {
	p, err := m.pool(ctx, m.admin())
	if err != nil {
		return nil, err
	}
	const q = `
		SELECT d.datname,
		       pg_catalog.pg_get_userbyid(d.datdba) AS owner,
		       pg_size_pretty(pg_database_size(d.datname)) AS size,
		       pg_database_size(d.datname) AS size_raw
		FROM pg_catalog.pg_database d
		WHERE d.datistemplate = false
		ORDER BY d.datname`
	rows, err := p.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Database
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.Name, &d.Owner, &d.Size, &d.SizeRaw); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Table describes a table or view within a schema.
type Table struct {
	Schema   string
	Name     string
	Kind     string // "table" or "view"
	Rows     int64  // estimated live rows
	Size     string
}

// ListTables returns user tables/views in the given database, excluding the
// internal pg_catalog and information_schema schemas.
func (m *Manager) ListTables(ctx context.Context, database string) ([]Table, error) {
	p, err := m.pool(ctx, database)
	if err != nil {
		return nil, err
	}
	const q = `
		SELECT n.nspname AS schema,
		       c.relname AS name,
		       CASE c.relkind WHEN 'r' THEN 'table' WHEN 'v' THEN 'view'
		            WHEN 'm' THEN 'matview' WHEN 'p' THEN 'table' ELSE c.relkind::text END AS kind,
		       COALESCE(c.reltuples, 0)::bigint AS est_rows,
		       pg_size_pretty(pg_total_relation_size(c.oid)) AS size
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r','v','m','p')
		  AND n.nspname NOT IN ('pg_catalog','information_schema')
		  AND n.nspname NOT LIKE 'pg_toast%'
		ORDER BY n.nspname, c.relname`
	rows, err := p.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Schema, &t.Name, &t.Kind, &t.Rows, &t.Size); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Column describes one column of a table.
type Column struct {
	Name     string
	Type     string
	Nullable bool
	Default  string
}

// TableColumns returns the columns of schema.table in definition order.
func (m *Manager) TableColumns(ctx context.Context, database, schema, table string) ([]Column, error) {
	p, err := m.pool(ctx, database)
	if err != nil {
		return nil, err
	}
	const q = `
		SELECT a.attname,
		       pg_catalog.format_type(a.atttypid, a.atttypmod) AS type,
		       NOT a.attnotnull AS nullable,
		       COALESCE(pg_get_expr(ad.adbin, ad.adrelid), '') AS default_expr
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_catalog.pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relname = $2
		  AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`
	rows, err := p.Query(ctx, q, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Column
	for rows.Next() {
		var col Column
		if err := rows.Scan(&col.Name, &col.Type, &col.Nullable, &col.Default); err != nil {
			return nil, err
		}
		out = append(out, col)
	}
	return out, rows.Err()
}

// Result is a generic tabular query result.
type Result struct {
	Columns  []string
	Rows     [][]string
	RowCount int
}

// PreviewTable returns up to limit rows of schema.table. Identifiers are
// validated and quoted to prevent injection.
func (m *Manager) PreviewTable(ctx context.Context, database, schema, table string, limit int) (*Result, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := fmt.Sprintf("SELECT * FROM %s.%s LIMIT %d",
		quoteIdent(schema), quoteIdent(table), limit)
	return m.Query(ctx, database, q)
}

// Query runs an arbitrary SQL statement against the given database and returns
// the result as strings. Non-SELECT statements return their command tag.
func (m *Manager) Query(ctx context.Context, database, sql string) (*Result, error) {
	p, err := m.pool(ctx, database)
	if err != nil {
		return nil, err
	}
	rows, err := p.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	res := &Result{Columns: make([]string, len(fields))}
	for i, f := range fields {
		res.Columns[i] = string(f.Name)
	}

	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		record := make([]string, len(vals))
		for i, v := range vals {
			record[i] = renderValue(v)
		}
		res.Rows = append(res.Rows, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.RowCount = len(res.Rows)

	// For statements without a row set (INSERT/UPDATE/etc.), surface the tag.
	if len(res.Columns) == 0 {
		tag := rows.CommandTag()
		res.Columns = []string{"result"}
		res.Rows = [][]string{{tag.String()}}
		res.RowCount = int(tag.RowsAffected())
	}
	return res, nil
}

// CreateDatabase creates a new database owned by the current user.
func (m *Manager) CreateDatabase(ctx context.Context, name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	p, err := m.pool(ctx, m.admin())
	if err != nil {
		return err
	}
	_, err = p.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", quoteIdent(name)))
	return err
}

// DropDatabase drops a database, terminating existing connections first.
func (m *Manager) DropDatabase(ctx context.Context, name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	if name == m.admin() {
		return fmt.Errorf("refusing to drop the admin database %q", name)
	}

	// Close and forget any cached pool to this database.
	m.mu.Lock()
	if pl, ok := m.pools[name]; ok {
		pl.Close()
		delete(m.pools, name)
	}
	m.mu.Unlock()

	p, err := m.pool(ctx, m.admin())
	if err != nil {
		return err
	}
	// Terminate other sessions so DROP can proceed.
	_, _ = p.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		 WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
	_, err = p.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdent(name)))
	return err
}

// SortedSchemas returns the distinct schema names present in tables, sorted.
func SortedSchemas(tables []Table) []string {
	seen := map[string]struct{}{}
	for _, t := range tables {
		seen[t.Schema] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// pgxConn is used to keep the pgx import referenced for potential future
// single-connection operations; harmless and documents the dependency.
var _ = pgx.ErrNoRows
