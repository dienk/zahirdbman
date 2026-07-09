// Package backup wraps the PostgreSQL client tools (pg_dump, pg_restore, psql)
// to provide logical backup and restore for zahirdbman.
package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/zahir/zahirdbman/internal/config"
)

// Format identifies a dump format.
type Format string

const (
	// FormatCustom is pg_dump's compressed custom archive (-Fc), restored with
	// pg_restore. Recommended: selective, parallelizable, self-describing.
	FormatCustom Format = "custom"
	// FormatPlain is a plain SQL script (-Fp), restored with psql.
	FormatPlain Format = "plain"
)

// Ext returns the conventional file extension for the format.
func (f Format) Ext() string {
	if f == FormatPlain {
		return "sql"
	}
	return "dump"
}

// Tools resolves and runs the PostgreSQL client binaries.
type Tools struct {
	cfg        config.Config
	pgDump     string
	pgRestore  string
	psql       string
}

// New locates the client binaries on PATH. Missing tools are recorded as empty
// paths; Available reports overall readiness.
func New(cfg config.Config) *Tools {
	look := func(name string) string {
		p, err := exec.LookPath(name)
		if err != nil {
			return ""
		}
		return p
	}
	return &Tools{
		cfg:       cfg,
		pgDump:    look("pg_dump"),
		pgRestore: look("pg_restore"),
		psql:      look("psql"),
	}
}

// Available reports whether all required tools were found.
func (t *Tools) Available() bool {
	return t.pgDump != "" && t.pgRestore != "" && t.psql != ""
}

// Missing lists any tools that could not be found on PATH.
func (t *Tools) Missing() []string {
	var m []string
	for name, path := range map[string]string{"pg_dump": t.pgDump, "pg_restore": t.pgRestore, "psql": t.psql} {
		if path == "" {
			m = append(m, name)
		}
	}
	return m
}

// env returns the process environment augmented with libpq connection secrets
// so the tools authenticate without exposing the password on the command line.
func (t *Tools) env() []string {
	return append(os.Environ(),
		"PGPASSWORD="+t.cfg.PGPassword,
		"PGSSLMODE="+t.cfg.PGSSLMode,
	)
}

// connArgs are the shared -h/-p/-U flags for every tool invocation.
func (t *Tools) connArgs() []string {
	return []string{"-h", t.cfg.PGHost, "-p", t.cfg.PGPort, "-U", t.cfg.PGUser}
}

// Dump runs pg_dump for the given database and streams the archive to w.
// stderr is captured and returned in the error if the command fails.
func (t *Tools) Dump(ctx context.Context, w io.Writer, database string, format Format) error {
	if t.pgDump == "" {
		return fmt.Errorf("pg_dump not found on PATH")
	}
	args := t.connArgs()
	if format == FormatPlain {
		args = append(args, "-Fp")
	} else {
		args = append(args, "-Fc")
	}
	args = append(args, "-d", database)

	var stderr capBuf
	cmd := exec.CommandContext(ctx, t.pgDump, args...)
	cmd.Env = t.env()
	cmd.Stdout = w
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump failed: %w: %s", err, stderr.String())
	}
	return nil
}

// RestoreOptions tune a restore operation.
type RestoreOptions struct {
	// Clean drops existing objects before recreating them (--clean --if-exists
	// for pg_restore). Ignored for plain SQL restores.
	Clean bool
	// NoOwner skips ownership assignment, useful across differing role sets.
	NoOwner bool
}

// Restore loads a dump (read from r) into the target database. The format must
// match how the dump was produced. The archive is spooled to a temp file so
// pg_restore can seek within it.
func (t *Tools) Restore(ctx context.Context, database string, format Format, r io.Reader, opts RestoreOptions) error {
	tmp, err := os.CreateTemp("", "zdbm-restore-*."+format.Ext())
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return fmt.Errorf("spool upload: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("flush upload: %w", err)
	}

	var bin string
	args := t.connArgs()
	if format == FormatPlain {
		if t.psql == "" {
			return fmt.Errorf("psql not found on PATH")
		}
		bin = t.psql
		args = append(args, "-v", "ON_ERROR_STOP=1", "-d", database, "-f", tmp.Name())
	} else {
		if t.pgRestore == "" {
			return fmt.Errorf("pg_restore not found on PATH")
		}
		bin = t.pgRestore
		args = append(args, "-d", database)
		if opts.Clean {
			args = append(args, "--clean", "--if-exists")
		}
		if opts.NoOwner {
			args = append(args, "--no-owner")
		}
		args = append(args, tmp.Name())
	}

	var stderr capBuf
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = t.env()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restore failed: %w: %s", err, stderr.String())
	}
	return nil
}

// capBuf is a bounded buffer that keeps at most 8 KiB of tool stderr so a
// runaway process cannot exhaust memory while still surfacing the error text.
type capBuf struct {
	b   []byte
	max int
}

func (c *capBuf) Write(p []byte) (int, error) {
	if c.max == 0 {
		c.max = 8 << 10
	}
	if room := c.max - len(c.b); room > 0 {
		if len(p) <= room {
			c.b = append(c.b, p...)
		} else {
			c.b = append(c.b, p[:room]...)
		}
	}
	return len(p), nil
}

func (c *capBuf) String() string { return string(c.b) }
