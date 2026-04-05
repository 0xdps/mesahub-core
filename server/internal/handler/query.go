// Package handler — query.go handles POST /api/db/:name/query.
package handler

import (
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/0xdps/sqlite-hub/server/internal/auth"
	"github.com/0xdps/sqlite-hub/server/internal/cache"
	"github.com/0xdps/sqlite-hub/server/internal/config"
	"github.com/0xdps/sqlite-hub/server/internal/db"
	"github.com/0xdps/sqlite-hub/server/internal/telemetry"
)

// blockedQueryPattern matches SQL statements not permitted on the /query endpoint.
// VACUUM, REPLACE and UPSERT are included: they mutate data and must go through /exec.
var blockedQueryPattern = regexp.MustCompile(
	`(?i)^\s*(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|ATTACH|DETACH|VACUUM|REPLACE|UPSERT|PRAGMA\s+\w+\s*=)`)

// QueryHandler holds dependencies for read-only query execution.
type QueryHandler struct {
	cfg      *config.Config
	pool     *db.Pool
	registry *db.Registry
	cache    cache.Client
	tel      *telemetry.Counters
}

// NewQueryHandler creates a QueryHandler.
func NewQueryHandler(cfg *config.Config, pool *db.Pool, registry *db.Registry, c cache.Client, tel *telemetry.Counters) *QueryHandler {
	return &QueryHandler{cfg: cfg, pool: pool, registry: registry, cache: c, tel: tel}
}

// Query handles POST /api/db/:name/query.
func (h *QueryHandler) Query(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	rec, err := h.registry.GetDatabase(name)
	if err != nil || rec == nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return
	}
	if code, msg := auth.AuthorizeDB(r, h.cfg, h.cache, rec); code != 0 {
		ErrorJSON(w, code, msg)
		return
	}

	var body struct {
		SQL      string `json:"sql"`
		Bindings []any  `json:"bindings"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if body.SQL == "" {
		ErrorJSON(w, http.StatusBadRequest, "sql is required")
		return
	}
	if h.cfg.MaxSQLLength > 0 && len(body.SQL) > h.cfg.MaxSQLLength {
		ErrorJSON(w, http.StatusBadRequest, "sql exceeds maximum allowed length")
		return
	}
	if blockedQueryPattern.MatchString(body.SQL) {
		ErrorJSON(w, http.StatusForbidden,
			"This SQL statement is not allowed on the /query endpoint. Use /exec instead.")
		return
	}

	maxBindings := h.cfg.MaxSQLBindings
	if maxBindings <= 0 {
		maxBindings = 5000
	}
	if len(body.Bindings) > maxBindings {
		ErrorJSON(w, http.StatusBadRequest, "too many SQL bindings")
		return
	}

	sqlDB, err := h.pool.Get(name)
	if err != nil {
		h.tel.IncError()
		ErrorJSON(w, http.StatusInternalServerError, "failed to open database: "+err.Error())
		return
	}

	start := time.Now()
	rows, err := sqlDB.QueryContext(r.Context(), body.SQL, anySliceToDriverValues(body.Bindings)...)
	if err != nil {
		h.tel.IncError()
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	defer rows.Close()

	headers, rowData, rowsRead, err := scanRows(rows)
	if err != nil {
		h.tel.IncError()
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	elapsed := time.Since(start).Milliseconds()
	h.tel.IncRead(elapsed)

	writeJSON(w, http.StatusOK, map[string]any{
		"headers": headers,
		"rows":    rowData,
		"stat": map[string]any{
			"rowsAffected":    0,
			"rowsRead":        rowsRead,
			"rowsWritten":     nil,
			"queryDurationMs": elapsed,
		},
	})
}

// QueryByUUID handles POST /api/query/:uuid (control mode only).
// It resolves the UUID to a template-internal database name via control.db
// and then runs the same read-only query logic as Query.
func (h *QueryHandler) QueryByUUID(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")

	templateName, ownerID, err := auth.LookupByUUID(h.cfg.DataPath, uuid)
	if err != nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return
	}

	rec, err := h.registry.GetDatabase(templateName)
	if err != nil || rec == nil {
		// DB exists in control.db but not yet registered (created before auto-register).
		// Register it on the fly so subsequent requests also succeed.
		if _, regErr := h.registry.InsertDatabase(templateName, ownerID, nil, nil); regErr == nil {
			rec, _ = h.registry.GetDatabase(templateName)
		}
	}
	if rec == nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return
	}
	if code, msg := auth.AuthorizeDBByUUID(r, h.cfg, h.cache, rec, uuid); code != 0 {
		ErrorJSON(w, code, msg)
		return
	}

	var body struct {
		SQL      string `json:"sql"`
		Bindings []any  `json:"bindings"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if body.SQL == "" {
		ErrorJSON(w, http.StatusBadRequest, "sql is required")
		return
	}
	if h.cfg.MaxSQLLength > 0 && len(body.SQL) > h.cfg.MaxSQLLength {
		ErrorJSON(w, http.StatusBadRequest, "sql exceeds maximum allowed length")
		return
	}
	if blockedQueryPattern.MatchString(body.SQL) {
		ErrorJSON(w, http.StatusForbidden,
			"This SQL statement is not allowed on the /query endpoint. Use /exec instead.")
		return
	}

	maxBindings := h.cfg.MaxSQLBindings
	if maxBindings <= 0 {
		maxBindings = 5000
	}
	if len(body.Bindings) > maxBindings {
		ErrorJSON(w, http.StatusBadRequest, "too many SQL bindings")
		return
	}

	sqlDB, err := h.pool.Get(templateName)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "failed to open database: "+err.Error())
		return
	}

	start := time.Now()
	rows, err := sqlDB.QueryContext(r.Context(), body.SQL, anySliceToDriverValues(body.Bindings)...)
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	defer rows.Close()

	headers, rowData, rowsRead, err := scanRows(rows)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	elapsed := time.Since(start).Milliseconds()

	writeJSON(w, http.StatusOK, map[string]any{
		"headers": headers,
		"rows":    rowData,
		"stat": map[string]any{
			"rowsAffected":    0,
			"rowsRead":        rowsRead,
			"rowsWritten":     nil,
			"queryDurationMs": elapsed,
		},
	})
}
