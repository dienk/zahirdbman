package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/zahir/zahirdbman/internal/profile"
)

// The /api/* endpoints return JSON for external clients such as the Vercel
// frontend. Cross-origin access is gated by the ZDBM_CORS_ORIGIN setting
// (see CORS in cmd/server); without it, browsers on other origins are blocked.

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// apiServer returns version/uptime info for the active connection.
func (h *Handler) apiServer(w http.ResponseWriter, r *http.Request) {
	c, cancel := ctx(r)
	defer cancel()
	info, err := h.mgr.ServerInfo(c)
	if err != nil {
		writeAPIErr(w, http.StatusBadGateway, err.Error())
		return
	}
	ci := h.mgr.ConnInfo()
	writeJSON(w, http.StatusOK, map[string]any{
		"server": info,
		"connection": map[string]string{
			"profile": h.profiles.ActiveName(),
			"host":    ci.Host,
			"port":    ci.Port,
			"user":    ci.User,
			"adminDB": ci.AdminDB,
		},
	})
}

// apiDatabases lists databases on the active connection.
func (h *Handler) apiDatabases(w http.ResponseWriter, r *http.Request) {
	c, cancel := ctx(r)
	defer cancel()
	dbs, err := h.mgr.ListDatabases(c)
	if err != nil {
		writeAPIErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"databases": dbs})
}

// apiTables lists tables/views in ?db=NAME.
func (h *Handler) apiTables(w http.ResponseWriter, r *http.Request) {
	db := r.URL.Query().Get("db")
	if db == "" {
		writeAPIErr(w, http.StatusBadRequest, "missing db parameter")
		return
	}
	c, cancel := ctx(r)
	defer cancel()
	tables, err := h.mgr.ListTables(c, db)
	if err != nil {
		writeAPIErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": tables})
}

// connView is a profile as returned to clients — without the password.
type connView struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    string `json:"port"`
	User    string `json:"user"`
	AdminDB string `json:"adminDB"`
	SSLMode string `json:"sslmode"`
	Active  bool   `json:"active"`
}

// apiConnections lists the saved connection profiles (passwords omitted).
func (h *Handler) apiConnections(w http.ResponseWriter, r *http.Request) {
	active := h.profiles.ActiveName()
	var out []connView
	for _, p := range h.profiles.List() {
		out = append(out, connView{
			Name: p.Name, Host: p.Host, Port: p.Port, User: p.User,
			AdminDB: p.AdminDB, SSLMode: p.SSLMode, Active: p.Name == active,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": out, "active": active})
}

// apiConnSave creates or updates a profile, optionally activating it.
func (h *Handler) apiConnSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErr(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var req struct {
		profile.Profile
		Activate bool `json:"activate"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPIErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if err := h.profiles.Upsert(req.Profile); err != nil {
		writeAPIErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Activate {
		c, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		if err := h.doActivate(c, req.Name); err != nil {
			// Saved, but couldn't connect — report as a soft error.
			writeJSON(w, http.StatusOK, map[string]any{"saved": true, "activated": false, "error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "activated": req.Activate})
}

// apiConnActivate switches the active connection to the named profile.
func (h *Handler) apiConnActivate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErr(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeAPIErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	c, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err := h.doActivate(c, req.Name); err != nil {
		writeAPIErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"active": req.Name})
}

// apiConnDelete removes a profile.
func (h *Handler) apiConnDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErr(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeAPIErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if err := h.profiles.Delete(req.Name); err != nil {
		writeAPIErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": req.Name})
}

// apiQuery runs a SQL statement against a database.
func (h *Handler) apiQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErr(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var req struct {
		DB  string `json:"db"`
		SQL string `json:"sql"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPIErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.SQL == "" {
		writeAPIErr(w, http.StatusBadRequest, "missing sql")
		return
	}
	c, cancel := ctx(r)
	defer cancel()
	res, err := h.mgr.Query(c, req.DB, req.SQL)
	if err != nil {
		writeAPIErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
