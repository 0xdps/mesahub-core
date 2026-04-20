package handler_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/0xdps/sqlite-hub/server/internal/handler"
)

// queryRouter wires QueryHandler with admin-stamp middleware.
func queryRouter(e *testEnv) *chi.Mux {
	h := handler.NewQueryHandler(e.cfg, e.pool, e.registry, e.cache, e.tel)
	return adminRouter(e, func(r chi.Router) {
		r.Post("/api/db/{name}/query", h.Query)
	})
}

// seedQueryDB registers a db and seeds a table for query tests.
func seedQueryDB(t *testing.T, e *testEnv, dbName string) {
	t.Helper()
	if _, err := e.registry.InsertDatabase(dbName, "tester", nil); err != nil {
		t.Fatalf("InsertDatabase: %v", err)
	}
	sqlDB, err := e.pool.Get(dbName)
	if err != nil {
		t.Fatalf("pool.Get: %v", err)
	}
	sqlDB.Exec(`CREATE TABLE products (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, price REAL)`)
	sqlDB.Exec(`INSERT INTO products (name, price) VALUES ('widget', 9.99)`)
	sqlDB.Exec(`INSERT INTO products (name, price) VALUES ('gadget', 19.99)`)
}

// ── SELECT (allowed) ──────────────────────────────────────────────────────────

func TestQueryHandler_Select_OK(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "SELECT * FROM products",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d; want 200 — body: %v", code, resp)
	}
	rows, ok := resp["rows"].([]any)
	if !ok {
		t.Fatalf("rows not slice: %v", resp["rows"])
	}
	if len(rows) != 2 {
		t.Errorf("len(rows) = %d; want 2", len(rows))
	}
}

func TestQueryHandler_Select_Headers(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "SELECT id, name FROM products LIMIT 1",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	headers, ok := resp["headers"].([]any)
	if !ok || len(headers) != 2 {
		t.Errorf("expected 2 headers, got %v", resp["headers"])
	}
}

func TestQueryHandler_Select_WithBindings(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql":      "SELECT * FROM products WHERE name = ?",
		"bindings": []any{"widget"},
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d — body: %v", code, resp)
	}
	rows, _ := resp["rows"].([]any)
	if len(rows) != 1 {
		t.Errorf("len(rows) = %d; want 1", len(rows))
	}
}

func TestQueryHandler_WithExpression(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "SELECT 1+1 AS result",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d — body: %v", code, resp)
	}
	rows, _ := resp["rows"].([]any)
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}
}

// ── PRAGMA (read-only PRAGMA is allowed) ──────────────────────────────────────

func TestQueryHandler_Pragma_Read_OK(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "PRAGMA table_info(products)",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d; want 200 — body: %v", code, resp)
	}
	rows, _ := resp["rows"].([]any)
	if len(rows) == 0 {
		t.Error("expected PRAGMA table_info rows")
	}
}

// ── blocked write statements ──────────────────────────────────────────────────

func TestQueryHandler_INSERT_Blocked(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "INSERT INTO products (name, price) VALUES ('evil', 0)",
	})
	if code == http.StatusOK {
		t.Error("INSERT should be blocked on query endpoint")
	}
}

func TestQueryHandler_UPDATE_Blocked(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "UPDATE products SET price = 0 WHERE id = 1",
	})
	if code == http.StatusOK {
		t.Error("UPDATE should be blocked on query endpoint")
	}
}

func TestQueryHandler_DELETE_Blocked(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "DELETE FROM products WHERE id = 1",
	})
	if code == http.StatusOK {
		t.Error("DELETE should be blocked on query endpoint")
	}
}

func TestQueryHandler_DROP_Blocked(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "DROP TABLE products",
	})
	if code == http.StatusOK {
		t.Error("DROP should be blocked on query endpoint")
	}
}

func TestQueryHandler_CREATE_Blocked(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "CREATE TABLE evil (id INTEGER)",
	})
	if code == http.StatusOK {
		t.Error("CREATE should be blocked on query endpoint")
	}
}

func TestQueryHandler_ALTER_Blocked(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "ALTER TABLE products ADD COLUMN extra TEXT",
	})
	if code == http.StatusOK {
		t.Error("ALTER should be blocked on query endpoint")
	}
}

func TestQueryHandler_Pragma_Write_Blocked(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "PRAGMA journal_mode = WAL",
	})
	if code == http.StatusOK {
		t.Error("PRAGMA key=val should be blocked on query endpoint")
	}
}

// ── comment bypass attempts ───────────────────────────────────────────────────

func TestQueryHandler_CommentBypass_INSERT_Blocked(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	// Attempt to slip INSERT past comment stripping.
	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "/* harmless */ INSERT INTO products (name, price) VALUES ('x', 0)",
	})
	if code == http.StatusOK {
		t.Error("comment-prefixed INSERT should still be blocked")
	}
}

func TestQueryHandler_LineCommentBypass_INSERT_Blocked(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "-- start\nINSERT INTO products (name, price) VALUES ('x', 0)",
	})
	if code == http.StatusOK {
		t.Error("line-comment-prefixed INSERT should still be blocked")
	}
}

// ── validation errors ─────────────────────────────────────────────────────────

func TestQueryHandler_EmptySQL(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "",
	})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 (empty sql)", code)
	}
}

func TestQueryHandler_SQLTooLong(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.MaxSQLLength = 5
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "SELECT * FROM products",
	})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 (sql too long)", code)
	}
}

func TestQueryHandler_TooManyBindings(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.MaxSQLBindings = 1
	seedQueryDB(t, e, "qdb")
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql":      "SELECT ?",
		"bindings": []any{1, 2},
	})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 (too many bindings)", code)
	}
}

func TestQueryHandler_DBNotFound(t *testing.T) {
	e := newTestEnv(t)
	router := queryRouter(e)

	code, _ := envFire(t, router, http.MethodPost, "/api/db/ghost/query", map[string]any{
		"sql": "SELECT 1",
	})
	if code != http.StatusNotFound {
		t.Errorf("status = %d; want 404", code)
	}
}

// ── auth ─────────────────────────────────────────────────────────────────────

func TestQueryHandler_NoAuth_Rejected(t *testing.T) {
	e := newTestEnv(t)
	seedQueryDB(t, e, "qdb")

	h := handler.NewQueryHandler(e.cfg, e.pool, e.registry, e.cache, e.tel)
	r := noAuthRouter(func(r chi.Router) {
		r.Post("/api/db/{name}/query", h.Query)
	})
	code, _ := envFire(t, r, http.MethodPost, "/api/db/qdb/query", map[string]any{
		"sql": "SELECT 1",
	})
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", code)
	}
}
