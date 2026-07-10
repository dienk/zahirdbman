package handler

import (
	"encoding/json"
	"net/http"
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
