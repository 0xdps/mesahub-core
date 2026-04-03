// Package handler — helpers.go holds utilities shared across all handlers.
package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// ── JSON ──────────────────────────────────────────────────────────────────────

// decodeJSON reads and decodes a JSON request body into v.
// On error it writes a 400 response and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

// decodeJSONOpt decodes the body if present; ignores EOF / empty body.
func decodeJSONOpt(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// ── SQL helpers ───────────────────────────────────────────────────────────────

// colHeader is the per-column metadata sent to clients.
type colHeader struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	OriginalType string `json:"originalType"`
	Type         string `json:"type"`
}

// scanRows drains a *sql.Rows result set into the wire format expected by
// /query and /exec. Caller must close rows after this returns.
func scanRows(rows *sql.Rows) (headers []colHeader, rowData []map[string]any, rowsRead int, err error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, 0, err
	}
	types, _ := rows.ColumnTypes()

	headers = make([]colHeader, len(cols))
	for i, c := range cols {
		dbType := ""
		if i < len(types) {
			dbType = types[i].DatabaseTypeName()
		}
		headers[i] = colHeader{
			Name:         c,
			DisplayName:  c,
			OriginalType: dbType,
			Type:         sqliteTypeToLogical(dbType),
		}
	}

	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}

	for rows.Next() {
		if err = rows.Scan(ptrs...); err != nil {
			return nil, nil, rowsRead, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = vals[i]
		}
		rowData = append(rowData, row)
		rowsRead++
	}
	if rowData == nil {
		rowData = []map[string]any{}
	}
	return headers, rowData, rowsRead, rows.Err()
}

func sqliteTypeToLogical(t string) string {
	switch t {
	case "INTEGER", "INT":
		return "number"
	case "REAL", "FLOAT", "DOUBLE", "NUMERIC":
		return "number"
	case "BLOB":
		return "blob"
	case "BOOLEAN":
		return "boolean"
	default:
		return "text"
	}
}

// anySliceToDriverValues converts a []any JSON-decoded binding list so that
// numeric JSON values become the native Go types sqlite3 expects.
func anySliceToDriverValues(bindings []any) []any {
	if len(bindings) == 0 {
		return nil
	}
	out := make([]any, len(bindings))
	copy(out, bindings)
	return out
}

// ── NullString helpers ────────────────────────────────────────────────────────

// nullStr converts a sql.NullString to *string (nil when invalid/empty).
func nullStr(s sql.NullString) any {
	if !s.Valid {
		return nil
	}
	return s.String
}

// ── Misc ──────────────────────────────────────────────────────────────────────

func itoa(n int) string { return strconv.Itoa(n) }

// queryInt reads an integer query parameter, returning def on missing/invalid.
func queryInt(q url.Values, key string, def int) int {
	v := q.Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
