// Package handler — query.go handles POST /api/db/:name/query.
package handler

import (
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/0xdps/sqlite-hub-template/auth"
	"github.com/0xdps/sqlite-hub-template/cache"
	"github.com/0xdps/sqlite-hub-template/config"
	"github.com/0xdps/sqlite-hub-template/db"
	"github.com/0xdps/sqlite-hub-template/telemetry"
)

// blockedQueryPattern matches SQL statements not permitted on the /query endpoint.
// VACUUM, REPLACE and UPSERT are included: they mutate data and must go through /exec.
var blockedQueryPattern = regexp.MustCompile(
	`(?i)^\s*(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|ATTACH|DETACH|VACUUM|REPLACE|UPSERT|PRAGMA\s+\w+\s*=)`)

// sqlBlockCommentRe matches /* ... */ block comments (non-greedy, including newlines).
var sqlBlockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)

// sqlLineCommentRe matches -- ... line comments.
var sqlLineCommentRe = regexp.MustCompile(`--[^\n]*`)

// stripSQLComments removes block and line comments from a SQL statement so that
// the blocked-pattern check cannot be bypassed with a leading /* */ or --.
func stripSQLComments(s string) string {
	s = sqlBlockCommentRe.ReplaceAllString(s, "")
	s = sqlLineCommentRe.ReplaceAllString(s, "")
	return s
}

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
	code, msg, kv := auth.AuthorizeDBWithKey(r, h.cfg, h.cache, rec)
	if code != 0 {
		ErrorJSON(w, code, msg)
		return
	}
	// Rate-limit API key requests (no-op when Redis is unavailable).
	if kv != nil {
		count, _ := h.cache.IncrRateLimit(r.Context(), kv.KeyID, time.Minute)
		if count > rateLimitPerMinute {
			ErrorJSON(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
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
	if blockedQueryPattern.MatchString(stripSQLComments(body.SQL)) {
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
