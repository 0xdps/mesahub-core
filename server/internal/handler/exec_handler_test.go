package handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/0xdps/sqlite-hub/server/internal/handler"
)

// execRouter wires ExecHandler with admin-stamp middleware.
func execRouter(e *testEnv) *chi.Mux {
	h := handler.NewExecHandler(e.cfg, e.pool, e.queue, e.registry, e.cache, e.tel)
	return adminRouter(e, func(r chi.Router) {
		r.Post("/api/db/{name}/exec", h.Exec)
	})
}

// seedExecDB registers a database and creates a simple table for exec tests.
func seedExecDB(t *testing.T, e *testEnv, dbName string) {
	t.Helper()
	if _, err := e.registry.InsertDatabase(dbName, "tester", nil); err != nil {
		t.Fatalf("InsertDatabase: %v", err)
	}
	sqlDB, err := e.pool.Get(dbName)
	if err != nil {
		t.Fatalf("pool.Get: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE items (
		id    INTEGER PRIMARY KEY AUTOINCREMENT,
		label TEXT NOT NULL,
		qty   INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	// Seed a few rows.
	for _, row := range []struct {
		l string
		q int
	}{
		{"apple", 10}, {"banana", 5}, {"cherry", 20},
	} {
		sqlDB.Exec(`INSERT INTO items (label, qty) VALUES (?, ?)`, row.l, row.q)
	}
}

// ── SELECT / read path ────────────────────────────────────────────────────────

func TestExecHandler_Select_OK(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "SELECT * FROM items",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d; want 200 — body: %v", code, resp)
	}
	rows, ok := resp["rows"].([]any)
	if !ok {
		t.Fatalf("rows not slice: %v", resp["rows"])
	}
	if len(rows) != 3 {
		t.Errorf("len(rows) = %d; want 3", len(rows))
	}
}

func TestExecHandler_Select_Headers(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "SELECT id, label FROM items LIMIT 1",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d — body: %v", code, resp)
	}
	headers, ok := resp["headers"].([]any)
	if !ok || len(headers) != 2 {
		t.Errorf("expected 2 headers, got %v", resp["headers"])
	}
}

func TestExecHandler_Select_WithBindings(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql":      "SELECT * FROM items WHERE label = ?",
		"bindings": []any{"apple"},
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d — body: %v", code, resp)
	}
	rows := resp["rows"].([]any)
	if len(rows) != 1 {
		t.Errorf("len(rows) = %d; want 1", len(rows))
	}
}

// ── INSERT / write path ───────────────────────────────────────────────────────

func TestExecHandler_Insert_OK(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "INSERT INTO items (label, qty) VALUES ('durian', 1)",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d; want 200 — body: %v", code, resp)
	}
	if resp["rowsAffected"] == nil && resp["rows"] == nil {
		t.Error("expected rowsAffected or rows in response")
	}
}

func TestExecHandler_Insert_RowsAffected(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "INSERT INTO items (label, qty) VALUES ('elderberry', 7)",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d — body: %v", code, resp)
	}
	rowsAffected, _ := resp["rowsAffected"].(float64)
	if int(rowsAffected) != 1 {
		t.Errorf("rowsAffected = %v; want 1", rowsAffected)
	}
}

func TestExecHandler_Insert_Returning(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "INSERT INTO items (label, qty) VALUES ('fig', 3) RETURNING *",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d — body: %v", code, resp)
	}
	rows, ok := resp["rows"].([]any)
	if !ok || len(rows) == 0 {
		t.Fatal("expected rows from RETURNING *")
	}
	row := rows[0].(map[string]any)
	if row["label"] != "fig" {
		t.Errorf("label = %q; want fig", row["label"])
	}
}

func TestExecHandler_Update_OK(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "UPDATE items SET qty = 99 WHERE label = 'apple'",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d — body: %v", code, resp)
	}
	rowsAffected, _ := resp["rowsAffected"].(float64)
	if int(rowsAffected) != 1 {
		t.Errorf("rowsAffected = %v; want 1", rowsAffected)
	}
}

func TestExecHandler_Delete_OK(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "DELETE FROM items WHERE label = 'banana'",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d — body: %v", code, resp)
	}
	rowsAffected, _ := resp["rowsAffected"].(float64)
	if int(rowsAffected) != 1 {
		t.Errorf("rowsAffected = %v; want 1", rowsAffected)
	}
}

func TestExecHandler_CreateTable(t *testing.T) {
	e := newTestEnv(t)
	if _, err := e.registry.InsertDatabase("ddldb", "alice", nil); err != nil {
		t.Fatal(err)
	}
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/ddldb/exec", map[string]any{
		"sql": "CREATE TABLE IF NOT EXISTS notes (id INTEGER PRIMARY KEY, body TEXT)",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d; want 200 — body: %v", code, resp)
	}
}

func TestExecHandler_Pragma_ReadPath(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "PRAGMA journal_mode",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d — body: %v", code, resp)
	}
	rows, _ := resp["rows"].([]any)
	if len(rows) == 0 {
		t.Error("expected PRAGMA result rows")
	}
}

// ── stat field ────────────────────────────────────────────────────────────────

func TestExecHandler_StatPresent(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "SELECT 1",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if resp["stat"] == nil {
		t.Error("expected stat field")
	}
}

// ── validation errors ─────────────────────────────────────────────────────────

func TestExecHandler_EmptySQL(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "",
	})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 (empty sql)", code)
	}
}

func TestExecHandler_SQLTooLong(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.MaxSQLLength = 10 // tiny limit
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "SELECT * FROM items",
	})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 (sql too long)", code)
	}
}

func TestExecHandler_TooManyBindings(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.MaxSQLBindings = 2
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql":      "SELECT ?",
		"bindings": []any{1, 2, 3},
	})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 (too many bindings)", code)
	}
}

func TestExecHandler_InvalidSQL(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "THIS IS NOT SQL AT ALL !!@#",
	})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 (invalid sql)", code)
	}
}

func TestExecHandler_DBNotFound(t *testing.T) {
	e := newTestEnv(t)
	router := execRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/ghost/exec", map[string]any{
		"sql": "SELECT 1",
	})
	if code != http.StatusNotFound {
		t.Errorf("status = %d; want 404", code)
	}
}

// ── auth ─────────────────────────────────────────────────────────────────────

func TestExecHandler_NoAuth_Rejected(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")

	// Build a router WITHOUT the admin stamp middleware.
	h := handler.NewExecHandler(e.cfg, e.pool, e.queue, e.registry, e.cache, e.tel)
	r := noAuthRouter(func(r chi.Router) {
		r.Post("/api/db/{name}/exec", h.Exec)
	})

	code, _ := envFire(t, r, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "SELECT 1",
	})
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 (no auth)", code)
	}
}

// ── last_insert_rowid ─────────────────────────────────────────────────────────

func TestExecHandler_LastInsertRowid(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "INSERT INTO items (label, qty) VALUES ('grape', 2)",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d — body: %v", code, resp)
	}
	rowid, _ := resp["lastInsertRowid"].(float64)
	if rowid == 0 {
		t.Errorf("lastInsertRowid = 0; want > 0")
	}
}

// ── persistence check ─────────────────────────────────────────────────────────

func TestExecHandler_WriteIsPersisted(t *testing.T) {
	e := newTestEnv(t)
	seedExecDB(t, e, "xdb")
	router := execRouter(e)

	envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "INSERT INTO items (label, qty) VALUES ('mango', 15)",
	})
	// Read back with a SELECT.
	code, resp := envFire(t, router, http.MethodPost, "/api/db/xdb/exec", map[string]any{
		"sql": "SELECT * FROM items WHERE label = 'mango'",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	rows, _ := resp["rows"].([]any)
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}
	_ = strings.NewReader // keep import used via rest_test.go parity
}
