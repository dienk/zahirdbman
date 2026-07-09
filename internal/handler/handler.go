// Package handler wires HTTP routes to the store and renders the HTML UI.
package handler

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/zahir/zahirdbman/internal/backup"
	"github.com/zahir/zahirdbman/internal/store"
)

// UI holds parsed templates and the static file handler. Templates and CSS are
// embedded so the binary is fully self-contained.
type Handler struct {
	mgr    *store.Manager
	tools  *backup.Tools
	tmpl   *template.Template
	static http.Handler
}

// New builds a Handler from an embedded web filesystem rooted at "web".
func New(mgr *store.Manager, tools *backup.Tools, webFS embed.FS) (*Handler, error) {
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"now": func() string { return time.Now().Format("2006-01-02 15:04:05") },
	}).ParseFS(webFS, "web/templates/*.html")
	if err != nil {
		return nil, err
	}
	staticFS, err := fs.Sub(webFS, "web/static")
	if err != nil {
		return nil, err
	}
	return &Handler{
		mgr:    mgr,
		tools:  tools,
		tmpl:   tmpl,
		static: http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))),
	}, nil
}

// Routes returns the configured HTTP mux.
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/static/", h.static)
	mux.HandleFunc("/", h.index)
	mux.HandleFunc("/db", h.database)
	mux.HandleFunc("/table", h.table)
	mux.HandleFunc("/query", h.query)
	mux.HandleFunc("/create", h.create)
	mux.HandleFunc("/drop", h.drop)
	mux.HandleFunc("/backups", h.backupsPage)
	mux.HandleFunc("/backup", h.backup)
	mux.HandleFunc("/restore", h.restore)
	mux.HandleFunc("/healthz", h.healthz)
	return mux
}

func ctx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 30*time.Second)
}

// index lists all databases on the server.
func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	c, cancel := ctx(r)
	defer cancel()

	version, _ := h.mgr.ServerVersion(c)
	dbs, err := h.mgr.ListDatabases(c)
	if err != nil {
		h.renderError(w, "Could not list databases", err)
		return
	}
	h.render(w, "index.html", map[string]any{
		"Title":     "Databases",
		"Version":   version,
		"Databases": dbs,
		"Flash":     r.URL.Query().Get("flash"),
	})
}

// database lists the tables within one database.
func (h *Handler) database(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	c, cancel := ctx(r)
	defer cancel()

	tables, err := h.mgr.ListTables(c, name)
	if err != nil {
		h.renderError(w, "Could not list tables in "+name, err)
		return
	}
	h.render(w, "database.html", map[string]any{
		"Title":    name,
		"Database": name,
		"Schemas":  store.SortedSchemas(tables),
		"Tables":   tables,
	})
}

// table shows a table's columns and a preview of its rows.
func (h *Handler) table(w http.ResponseWriter, r *http.Request) {
	db := r.URL.Query().Get("db")
	schema := r.URL.Query().Get("schema")
	name := r.URL.Query().Get("name")
	if db == "" || schema == "" || name == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 100
	}
	c, cancel := ctx(r)
	defer cancel()

	cols, err := h.mgr.TableColumns(c, db, schema, name)
	if err != nil {
		h.renderError(w, "Could not read columns", err)
		return
	}
	preview, err := h.mgr.PreviewTable(c, db, schema, name, limit)
	if err != nil {
		h.renderError(w, "Could not preview rows", err)
		return
	}
	h.render(w, "table.html", map[string]any{
		"Title":    schema + "." + name,
		"Database": db,
		"Schema":   schema,
		"Table":    name,
		"Columns":  cols,
		"Preview":  preview,
		"Limit":    limit,
	})
}

// query runs the SQL console.
func (h *Handler) query(w http.ResponseWriter, r *http.Request) {
	db := r.URL.Query().Get("db")
	if db == "" {
		db = r.FormValue("db")
	}
	data := map[string]any{
		"Title":    "SQL Console",
		"Database": db,
	}
	if r.Method == http.MethodPost {
		sql := r.FormValue("sql")
		data["SQL"] = sql
		if sql != "" {
			c, cancel := ctx(r)
			defer cancel()
			res, err := h.mgr.Query(c, db, sql)
			if err != nil {
				data["QueryError"] = err.Error()
			} else {
				data["Result"] = res
			}
		}
	}
	h.render(w, "query.html", data)
}

// create makes a new database and redirects home.
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	name := r.FormValue("name")
	c, cancel := ctx(r)
	defer cancel()
	if err := h.mgr.CreateDatabase(c, name); err != nil {
		h.renderError(w, "Could not create database", err)
		return
	}
	http.Redirect(w, r, "/?flash=Created+database+"+name, http.StatusSeeOther)
}

// drop deletes a database and redirects home.
func (h *Handler) drop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	name := r.FormValue("name")
	c, cancel := ctx(r)
	defer cancel()
	if err := h.mgr.DropDatabase(c, name); err != nil {
		h.renderError(w, "Could not drop database", err)
		return
	}
	http.Redirect(w, r, "/?flash=Dropped+database+"+name, http.StatusSeeOther)
}

// backupsPage renders the Backup / Restore menu.
func (h *Handler) backupsPage(w http.ResponseWriter, r *http.Request) {
	c, cancel := ctx(r)
	defer cancel()

	dbs, err := h.mgr.ListDatabases(c)
	if err != nil {
		h.renderError(w, "Could not list databases", err)
		return
	}
	h.render(w, "backups.html", map[string]any{
		"Title":     "Backup & Restore",
		"Databases": dbs,
		"Selected":  r.URL.Query().Get("db"),
		"Available": h.tools.Available(),
		"Missing":   h.tools.Missing(),
		"Flash":     r.URL.Query().Get("flash"),
		"RestoreErr": r.URL.Query().Get("err"),
	})
}

// backup streams a pg_dump of the requested database to the browser as a
// file download.
func (h *Handler) backup(w http.ResponseWriter, r *http.Request) {
	db := r.URL.Query().Get("db")
	if db == "" {
		http.Redirect(w, r, "/backups", http.StatusSeeOther)
		return
	}
	format := backup.FormatCustom
	if r.URL.Query().Get("format") == string(backup.FormatPlain) {
		format = backup.FormatPlain
	}

	// pg_dump can run long; give it a generous timeout independent of the UI.
	c, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	// Download headers are set lazily on the first byte pg_dump emits, so if the
	// dump fails before producing output (e.g. missing database) we can still
	// return a clean error response instead of a half-written download.
	filename := db + "-" + time.Now().Format("20060102-150405") + "." + format.Ext()
	dw := &lazyDownload{w: w, filename: filename}

	if err := h.tools.Dump(c, dw, db, format); err != nil {
		if dw.wrote {
			// Streaming already began; the client sees a truncated file. Nothing
			// clean to send now — record it and move on.
			log.Printf("backup of %q aborted after %d bytes: %v", db, dw.n, err)
			return
		}
		http.Error(w, "backup failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// restore accepts an uploaded dump and loads it into a target database.
func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/backups", http.StatusSeeOther)
		return
	}
	// Cap uploads at 512 MiB.
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Redirect(w, r, "/backups?err="+urlq("upload too large or malformed: "+err.Error()), http.StatusSeeOther)
		return
	}

	target := r.FormValue("target")
	format := backup.FormatCustom
	if r.FormValue("format") == string(backup.FormatPlain) {
		format = backup.FormatPlain
	}
	opts := backup.RestoreOptions{
		Clean:   r.FormValue("clean") == "on",
		NoOwner: r.FormValue("no_owner") == "on",
	}

	file, _, err := r.FormFile("dump")
	if err != nil {
		http.Redirect(w, r, "/backups?err="+urlq("no dump file provided"), http.StatusSeeOther)
		return
	}
	defer file.Close()

	c, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	// Optionally create the target database first.
	if r.FormValue("create") == "on" {
		if err := h.mgr.CreateDatabase(c, target); err != nil {
			http.Redirect(w, r, "/backups?err="+urlq("create target: "+err.Error()), http.StatusSeeOther)
			return
		}
	}

	if err := h.tools.Restore(c, target, format, file, opts); err != nil {
		http.Redirect(w, r, "/backups?err="+urlq(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/backups?flash="+urlq("Restored into "+target), http.StatusSeeOther)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	c, cancel := ctx(r)
	defer cancel()
	if err := h.mgr.Ping(c); err != nil {
		http.Error(w, "unhealthy: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok"))
}

// urlq escapes a string for safe inclusion in a redirect query parameter.
func urlq(s string) string { return url.QueryEscape(s) }

// lazyDownload is an io.Writer wrapping an http.ResponseWriter that emits the
// file-download headers only when the first byte is written. This lets callers
// send a clean error status if the producing command fails before any output.
type lazyDownload struct {
	w        http.ResponseWriter
	filename string
	wrote    bool
	n        int
}

func (d *lazyDownload) Write(p []byte) (int, error) {
	if !d.wrote {
		d.wrote = true
		d.w.Header().Set("Content-Type", "application/octet-stream")
		d.w.Header().Set("Content-Disposition", "attachment; filename=\""+d.filename+"\"")
	}
	n, err := d.w.Write(p)
	d.n += n
	return n, err
}

func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) renderError(w http.ResponseWriter, msg string, err error) {
	w.WriteHeader(http.StatusBadGateway)
	h.render(w, "error.html", map[string]any{
		"Title":   "Error",
		"Message": msg,
		"Detail":  err.Error(),
	})
}
