// Package handler — rest.go implements a PostgREST-style auto-REST layer.
//
// Routes:
//
//	GET    /api/db/:name/rest/:table   — SELECT rows
//	POST   /api/db/:name/rest/:table   — INSERT row (JSON body) RETURNING *
//	PATCH  /api/db/:name/rest/:table   — UPDATE rows (body = SET, query = WHERE)
//	DELETE /api/db/:name/rest/:table   — DELETE rows (query = WHERE)
//
// Control-mode UUID equivalents:
//
//	GET    /api/rest/:ref/:table
//	POST   /api/rest/:ref/:table
//	PATCH  /api/rest/:ref/:table
//	DELETE /api/rest/:ref/:table
//
// # Filter syntax (query params)
//
// Any query param whose key is not a reserved word is treated as a column filter:
//
//	?col=val          → col = ?
//	?col=eq.val       → col = ?
//	?col=neq.val      → col != ?
//	?col=gt.val       → col > ?
//	?col=gte.val      → col >= ?
//	?col=lt.val       → col < ?
//	?col=lte.val      → col <= ?
//	?col=like.val     → col LIKE ?
//	?col=is.null      → col IS NULL
//	?col=not.null     → col IS NOT NULL
//
// # Reserved params
//
//	select=col1,col2           — column projection
//	order=col.asc,col2.desc    — ORDER BY
//	limit=N                    — LIMIT  (default 100, max 1000)
//	offset=N                   — OFFSET (default 0)
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/0xdps/mesahub-core/auth"
	"github.com/0xdps/mesahub-core/cache"
	"github.com/0xdps/mesahub-core/config"
	"github.com/0xdps/mesahub-core/db"
	"github.com/0xdps/mesahub-core/queue"
	"github.com/0xdps/mesahub-core/telemetry"
)

// restReserved lists query params that are not treated as column filters.
var restReserved = map[string]struct{}{
	"select": {},
	"order":  {},
	"limit":  {},
	"offset": {},
}

const (
	restDefaultLimit = 100
	restMaxLimit     = 1000
)

// RestHandler holds dependencies for the auto-REST layer.
type RestHandler struct {
	cfg      *config.Config
	pool     *db.Pool
	registry *db.Registry
	queue    *queue.Queue
	cache    cache.Client
	tel      *telemetry.Counters
}

// NewRestHandler creates a RestHandler.
func NewRestHandler(
	cfg *config.Config,
	pool *db.Pool,
	wq *queue.Queue,
	registry *db.Registry,
	c cache.Client,
	tel *telemetry.Counters,
) *RestHandler {
	return &RestHandler{
		cfg:      cfg,
		pool:     pool,
		registry: registry,
		queue:    wq,
		cache:    c,
		tel:      tel,
	}
}

// ── Named-DB route handlers ───────────────────────────────────────────────────

func (h *RestHandler) Get(w http.ResponseWriter, r *http.Request) {
	name, table, ok := h.resolveDB(w, r)
	if !ok {
		return
	}
	h.doGet(w, r, name, table)
}

func (h *RestHandler) Post(w http.ResponseWriter, r *http.Request) {
	name, table, ok := h.resolveDB(w, r)
	if !ok {
		return
	}
	h.doPost(w, r, name, table)
}

func (h *RestHandler) Patch(w http.ResponseWriter, r *http.Request) {
	name, table, ok := h.resolveDB(w, r)
	if !ok {
		return
	}
	h.doPatch(w, r, name, table)
}

func (h *RestHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name, table, ok := h.resolveDB(w, r)
	if !ok {
		return
	}
	h.doDelete(w, r, name, table)
}

// ── Auth helpers ──────────────────────────────────────────────────────────────

func (h *RestHandler) resolveDB(w http.ResponseWriter, r *http.Request) (dbName, table string, ok bool) {
	name := chi.URLParam(r, "name")
	table = chi.URLParam(r, "table")

	rec, err := h.registry.GetDatabase(name)
	if err != nil || rec == nil {
		ErrorJSON(w, http.StatusNotFound, "database not found")
		return "", "", false
	}
	code, msg, kv := auth.AuthorizeDBWithKey(r, h.cfg, h.cache, rec)
	if code != 0 {
		ErrorJSON(w, code, msg)
		return "", "", false
	}
	if kv != nil {
		count, _ := h.cache.IncrRateLimit(r.Context(), kv.KeyID, time.Minute)
		if count > rateLimitPerMinute {
			ErrorJSON(w, http.StatusTooManyRequests, "rate limit exceeded")
			return "", "", false
		}
	}
	return name, table, true
}

// ── Core operation implementations ───────────────────────────────────────────

func (h *RestHandler) doGet(w http.ResponseWriter, r *http.Request, dbName, table string) {
	if err := restValidateIdent(table); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "invalid table name")
		return
	}

	q := r.URL.Query()

	// SELECT columns
	selectSQL := "*"
	if sel := q.Get("select"); sel != "" {
		cols := strings.Split(sel, ",")
		quoted := make([]string, 0, len(cols))
		for _, c := range cols {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if err := restValidateIdent(c); err != nil {
				ErrorJSON(w, http.StatusBadRequest, "invalid column in select: "+c)
				return
			}
			quoted = append(quoted, restQuoteIdent(c))
		}
		if len(quoted) > 0 {
			selectSQL = strings.Join(quoted, ", ")
		}
	}

	// WHERE
	whereParts, bindings, err := buildRestWhere(q)
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	whereSql := ""
	if len(whereParts) > 0 {
		whereSql = " WHERE " + strings.Join(whereParts, " AND ")
	}

	// ORDER BY
	orderSQL, err := buildRestOrder(q.Get("order"))
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	// LIMIT / OFFSET
	limit := restDefaultLimit
	if lstr := q.Get("limit"); lstr != "" {
		v, parseErr := strconv.Atoi(lstr)
		if parseErr != nil || v < 0 {
			ErrorJSON(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if v > restMaxLimit {
			v = restMaxLimit
		}
		limit = v
	}
	offset := 0
	if ostr := q.Get("offset"); ostr != "" {
		v, parseErr := strconv.Atoi(ostr)
		if parseErr != nil || v < 0 {
			ErrorJSON(w, http.StatusBadRequest, "invalid offset")
			return
		}
		offset = v
	}

	sql := fmt.Sprintf(
		"SELECT %s FROM %s%s%s LIMIT %d OFFSET %d",
		selectSQL, restQuoteIdent(table), whereSql, orderSQL, limit, offset,
	)

	sqlDB, err := h.pool.Get(dbName)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "failed to open database")
		return
	}
	start := time.Now()
	rows, err := sqlDB.QueryContext(r.Context(), sql, anySliceToDriverValues(bindings)...)
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
			"rowsRead":        rowsRead,
			"queryDurationMs": elapsed,
		},
	})
}

func (h *RestHandler) doPost(w http.ResponseWriter, r *http.Request, dbName, table string) {
	if err := restValidateIdent(table); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "invalid table name")
		return
	}

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(data) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "body must contain at least one field")
		return
	}

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	cols := make([]string, 0, len(keys))
	bindings := make([]any, 0, len(keys))
	for _, k := range keys {
		if err := restValidateIdent(k); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "invalid column name: "+k)
			return
		}
		cols = append(cols, restQuoteIdent(k))
		bindings = append(bindings, data[k])
	}

	placeholders := strings.Repeat("?, ", len(cols))
	placeholders = strings.TrimSuffix(placeholders, ", ")
	sql := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) RETURNING *",
		restQuoteIdent(table), strings.Join(cols, ", "), placeholders,
	)

	sqlDB, err := h.pool.Get(dbName)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "failed to open database")
		return
	}

	var (
		headers []colHeader
		rowData []map[string]any
		elapsed int64
	)
	qErr := h.queue.Enqueue(r.Context(), dbName, func() error {
		start := time.Now()
		rows, queryErr := sqlDB.QueryContext(r.Context(), sql, anySliceToDriverValues(bindings)...)
		if queryErr != nil {
			return queryErr
		}
		defer rows.Close()
		var scanErr error
		headers, rowData, _, scanErr = scanRows(rows)
		elapsed = time.Since(start).Milliseconds()
		return scanErr
	})
	if qErr != nil {
		log.Error().Err(qErr).Str("db", dbName).Msg("[rest] insert error")
		h.tel.IncError()
		ErrorJSON(w, http.StatusBadRequest, qErr.Error())
		return
	}
	h.tel.IncWrite(elapsed)
	writeJSON(w, http.StatusCreated, map[string]any{
		"headers": headers,
		"rows":    rowData,
		"stat":    map[string]any{"queryDurationMs": elapsed},
	})
}

func (h *RestHandler) doPatch(w http.ResponseWriter, r *http.Request, dbName, table string) {
	if err := restValidateIdent(table); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "invalid table name")
		return
	}

	var setData map[string]any
	if err := json.NewDecoder(r.Body).Decode(&setData); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(setData) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "body must contain at least one field to set")
		return
	}

	setKeys := make([]string, 0, len(setData))
	for k := range setData {
		setKeys = append(setKeys, k)
	}
	sort.Strings(setKeys)

	setClauses := make([]string, 0, len(setKeys))
	setBindings := make([]any, 0, len(setKeys))
	for _, k := range setKeys {
		if err := restValidateIdent(k); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "invalid column name: "+k)
			return
		}
		setClauses = append(setClauses, restQuoteIdent(k)+" = ?")
		setBindings = append(setBindings, setData[k])
	}

	whereParts, whereBindings, err := buildRestWhere(r.URL.Query())
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(whereParts) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "PATCH requires at least one filter query param")
		return
	}

	bindings := append(setBindings, whereBindings...)
	sql := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s",
		restQuoteIdent(table), strings.Join(setClauses, ", "), strings.Join(whereParts, " AND "),
	)

	sqlDB, dbErr := h.pool.Get(dbName)
	if dbErr != nil {
		ErrorJSON(w, http.StatusInternalServerError, "failed to open database")
		return
	}

	var (
		rowsAffected int64
		elapsed      int64
	)
	qErr := h.queue.Enqueue(r.Context(), dbName, func() error {
		start := time.Now()
		res, execErr := sqlDB.ExecContext(r.Context(), sql, anySliceToDriverValues(bindings)...)
		if execErr != nil {
			return execErr
		}
		rowsAffected, _ = res.RowsAffected()
		elapsed = time.Since(start).Milliseconds()
		return nil
	})
	if qErr != nil {
		log.Error().Err(qErr).Str("db", dbName).Msg("[rest] update error")
		h.tel.IncError()
		ErrorJSON(w, http.StatusBadRequest, qErr.Error())
		return
	}
	h.tel.IncWrite(elapsed)
	writeJSON(w, http.StatusOK, map[string]any{
		"rowsAffected": rowsAffected,
		"stat":         map[string]any{"queryDurationMs": elapsed},
	})
}

func (h *RestHandler) doDelete(w http.ResponseWriter, r *http.Request, dbName, table string) {
	if err := restValidateIdent(table); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "invalid table name")
		return
	}

	whereParts, bindings, err := buildRestWhere(r.URL.Query())
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(whereParts) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "DELETE requires at least one filter query param")
		return
	}

	sql := fmt.Sprintf(
		"DELETE FROM %s WHERE %s",
		restQuoteIdent(table), strings.Join(whereParts, " AND "),
	)

	sqlDB, dbErr := h.pool.Get(dbName)
	if dbErr != nil {
		ErrorJSON(w, http.StatusInternalServerError, "failed to open database")
		return
	}

	var (
		rowsAffected int64
		elapsed      int64
	)
	qErr := h.queue.Enqueue(r.Context(), dbName, func() error {
		start := time.Now()
		res, execErr := sqlDB.ExecContext(r.Context(), sql, anySliceToDriverValues(bindings)...)
		if execErr != nil {
			return execErr
		}
		rowsAffected, _ = res.RowsAffected()
		elapsed = time.Since(start).Milliseconds()
		return nil
	})
	if qErr != nil {
		log.Error().Err(qErr).Str("db", dbName).Msg("[rest] delete error")
		h.tel.IncError()
		ErrorJSON(w, http.StatusBadRequest, qErr.Error())
		return
	}
	h.tel.IncWrite(elapsed)
	writeJSON(w, http.StatusOK, map[string]any{
		"rowsAffected": rowsAffected,
		"stat":         map[string]any{"queryDurationMs": elapsed},
	})
}

// ── SQL builder helpers ───────────────────────────────────────────────────────

// buildRestWhere converts non-reserved query params into parameterised WHERE clauses.
func buildRestWhere(q url.Values) (parts []string, bindings []any, err error) {
	// Collect keys in sorted order for deterministic SQL.
	keys := make([]string, 0, len(q))
	for k := range q {
		if _, reserved := restReserved[k]; !reserved {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		if err := restValidateIdent(key); err != nil {
			return nil, nil, fmt.Errorf("invalid filter column: %s", key)
		}
		val := q.Get(key)
		col := restQuoteIdent(key)

		switch {
		case val == "is.null":
			parts = append(parts, col+" IS NULL")
		case val == "not.null":
			parts = append(parts, col+" IS NOT NULL")
		case strings.HasPrefix(val, "eq."):
			parts = append(parts, col+" = ?")
			bindings = append(bindings, val[3:])
		case strings.HasPrefix(val, "neq."):
			parts = append(parts, col+" != ?")
			bindings = append(bindings, val[4:])
		case strings.HasPrefix(val, "gt."):
			parts = append(parts, col+" > ?")
			bindings = append(bindings, val[3:])
		case strings.HasPrefix(val, "gte."):
			parts = append(parts, col+" >= ?")
			bindings = append(bindings, val[4:])
		case strings.HasPrefix(val, "lt."):
			parts = append(parts, col+" < ?")
			bindings = append(bindings, val[3:])
		case strings.HasPrefix(val, "lte."):
			parts = append(parts, col+" <= ?")
			bindings = append(bindings, val[4:])
		case strings.HasPrefix(val, "like."):
			parts = append(parts, col+" LIKE ?")
			bindings = append(bindings, val[5:])
		default:
			// Plain value — equality shorthand.
			parts = append(parts, col+" = ?")
			bindings = append(bindings, val)
		}
	}
	return parts, bindings, nil
}

// buildRestOrder parses "col.asc,col2.desc" into an ORDER BY clause.
func buildRestOrder(order string) (string, error) {
	if order == "" {
		return "", nil
	}
	parts := strings.Split(order, ",")
	clauses := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments := strings.SplitN(part, ".", 2)
		col := segments[0]
		if err := restValidateIdent(col); err != nil {
			return "", fmt.Errorf("invalid order column: %s", col)
		}
		dir := "ASC"
		if len(segments) == 2 {
			switch strings.ToLower(segments[1]) {
			case "asc":
				dir = "ASC"
			case "desc":
				dir = "DESC"
			default:
				return "", fmt.Errorf("invalid order direction %q (use asc or desc)", segments[1])
			}
		}
		clauses = append(clauses, restQuoteIdent(col)+" "+dir)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " ORDER BY " + strings.Join(clauses, ", "), nil
}

// restQuoteIdent wraps an SQLite identifier in double quotes, escaping internal ones.
func restQuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// restValidateIdent rejects identifiers that are empty or suspiciously long.
// Quoting handles injection; this is an additional sanity guard.
func restValidateIdent(name string) error {
	if len(name) == 0 || len(name) > 128 {
		return fmt.Errorf("identifier length out of range: %q", name)
	}
	return nil
}
