// Package handler wires HTTP routes to the store and renders the HTML UI.
package handler

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/zahir/zahirdbman/internal/store"
)

// UI holds parsed templates and the static file handler. Templates and CSS are
// embedded so the binary is fully self-contained.
type Handler struct {
	mgr    *store.Manager
	tmpl   *template.Template
	static http.Handler
}

// New builds a Handler from an embedded web filesystem rooted at "web".
func New(mgr *store.Manager, webFS embed.FS) (*Handler, error) {
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

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	c, cancel := ctx(r)
	defer cancel()
	if err := h.mgr.Ping(c); err != nil {
		http.Error(w, "unhealthy: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok"))
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
