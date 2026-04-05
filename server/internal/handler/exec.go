// Package handler — exec.go handles POST /api/db/:name/exec.
package handler

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/0xdps/sqlite-hub/server/internal/auth"
	"github.com/0xdps/sqlite-hub/server/internal/cache"
	"github.com/0xdps/sqlite-hub/server/internal/config"
	"github.com/0xdps/sqlite-hub/server/internal/db"
	"github.com/0xdps/sqlite-hub/server/internal/queue"
)

// SQL classifiers (case-insensitive prefix match).
var (
	readSQLPat  = regexp.MustCompile(`(?i)^\s*(SELECT|WITH|VALUES|EXPLAIN|PRAGMA\s+\w+\s*([^=]|$))`)
	writeSQLPat = regexp.MustCompile(`(?i)^\s*(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|ATTACH|DETACH|REPLACE|UPSERT|PRAGMA\s+\w+\s*=)`)
)

func classifySQL(s string) string {
	trimmed := strings.TrimSpace(s)
	if readSQLPat.MatchString(trimmed) {
		return "read"
	}
	if writeSQLPat.MatchString(trimmed) {
		return "write"
	}
	return "unknown"
}

// ExecHandler holds dependencies for exec execution.
type ExecHandler struct {
	cfg      *config.Config
	pool     *db.Pool
	queue    *queue.Queue
	registry *db.Registry
	cache    cache.Client
}

// NewExecHandler creates an ExecHandler.
func NewExecHandler(cfg *config.Config, pool *db.Pool, wq *queue.Queue, registry *db.Registry, c cache.Client) *ExecHandler {
	return &ExecHandler{cfg: cfg, pool: pool, queue: wq, registry: registry, cache: c}
}

// Exec handles POST /api/db/:name/exec.
func (h *ExecHandler) Exec(w http.ResponseWriter, r *http.Request) {
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
	maxBindings := h.cfg.MaxSQLBindings
	if maxBindings <= 0 {
		maxBindings = 5000
	}
	if len(body.Bindings) > maxBindings {
		ErrorJSON(w, http.StatusBadRequest, "too many SQL bindings")
		return
	}

	if classifySQL(body.SQL) == "read" {
		h.execRead(w, r, name, body.SQL, body.Bindings)
	} else {
		h.execWrite(w, r, name, body.SQL, body.Bindings)
	}
}

func (h *ExecHandler) execRead(w http.ResponseWriter, r *http.Request, name, sqlStr string, bindings []any) {
	sqlDB, err := h.pool.Get(name)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "failed to open database")
		return
	}
	start := time.Now()
	rows, err := sqlDB.QueryContext(r.Context(), sqlStr, anySliceToDriverValues(bindings)...)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"headers": headers,
		"rows":    rowData,
		"stat": map[string]any{
			"rowsRead":        rowsRead,
			"queryDurationMs": time.Since(start).Milliseconds(),
		},
	})
}

func (h *ExecHandler) execWrite(w http.ResponseWriter, r *http.Request, name, sqlStr string, bindings []any) {
	sqlDB, err := h.pool.Get(name)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "failed to open database")
		return
	}

	// Result variables captured by the closure.
	var (
		headers         []colHeader
		rowData         []map[string]any
		rowsRead        int
		rowsAffected    int64
		lastInsertRowid int64
		isReader        bool
		elapsed         int64
	)

	qErr := h.queue.Enqueue(r.Context(), name, func() error {
		start := time.Now()
		args := anySliceToDriverValues(bindings)

		// Try Query first — handles RETURNING clauses and SELECT-like writes.
		rows, queryErr := sqlDB.QueryContext(r.Context(), sqlStr, args...)
		if queryErr == nil {
			cols, _ := rows.Columns()
			if len(cols) > 0 {
				isReader = true
				var scanErr error
				headers, rowData, rowsRead, scanErr = scanRows(rows)
				rows.Close()
				elapsed = time.Since(start).Milliseconds()
				return scanErr
			}
			rows.Close()
		}

		// Pure write: INSERT / UPDATE / DELETE without RETURNING.
		res, execErr := sqlDB.ExecContext(r.Context(), sqlStr, args...)
		if execErr != nil {
			return execErr
		}
		rowsAffected, _ = res.RowsAffected()
		lastInsertRowid, _ = res.LastInsertId()
		elapsed = time.Since(start).Milliseconds()
		return nil
	})

	if qErr != nil {
		log.Error().Err(qErr).Str("db", name).Msg("[exec] write queue error")
		ErrorJSON(w, http.StatusBadRequest, qErr.Error())
		return
	}

	if isReader {
		writeJSON(w, http.StatusOK, map[string]any{
			"headers": headers,
			"rows":    rowData,
			"stat": map[string]any{
				"rowsRead":        rowsRead,
				"queryDurationMs": elapsed,
			},
		})
	} else {
		writeJSON(w, http.StatusOK, map[string]any{
			"rowsAffected":    rowsAffected,
			"lastInsertRowid": lastInsertRowid,
			"stat": map[string]any{
				"queryDurationMs": elapsed,
			},
		})
	}
}

// ExecByUUID handles POST /api/exec/:uuid (control mode only).
// It resolves the UUID to a template-internal database name via control.db
// and then runs the same exec logic as Exec.
func (h *ExecHandler) ExecByUUID(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")

	templateName, _, err := auth.LookupByUUID(h.cfg.DataPath, uuid)
	if err != nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return
	}

	rec, err := h.registry.GetDatabase(templateName)
	if err != nil || rec == nil {
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
	maxBindings := h.cfg.MaxSQLBindings
	if maxBindings <= 0 {
		maxBindings = 5000
	}
	if len(body.Bindings) > maxBindings {
		ErrorJSON(w, http.StatusBadRequest, "too many SQL bindings")
		return
	}

	if classifySQL(body.SQL) == "read" {
		h.execRead(w, r, templateName, body.SQL, body.Bindings)
	} else {
		h.execWrite(w, r, templateName, body.SQL, body.Bindings)
	}
}
