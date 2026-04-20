package handler_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/0xdps/sqlite-hub/server/internal/handler"
)

// maintenanceRouter wires MaintenanceHandler routes with admin-stamp middleware.
func maintenanceRouter(e *testEnv) *chi.Mux {
	h := handler.NewMaintenanceHandler(e.cfg, e.registry, e.storage)
	return adminRouter(e, func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(handler.RequireAdmin)
			r.Post("/api/maintenance/cleanup", h.Cleanup)
		})
	})
}

// ── POST /api/maintenance/cleanup ────────────────────────────────────────────

func TestMaintenanceHandler_Cleanup_OK(t *testing.T) {
	e := newTestEnv(t)
	router := maintenanceRouter(e)

	code, resp := envFire(t, router, http.MethodPost, "/api/maintenance/cleanup", nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d; want 200 — body: %v", code, resp)
	}
}

func TestMaintenanceHandler_Cleanup_FilesStatus(t *testing.T) {
	e := newTestEnv(t)
	router := maintenanceRouter(e)

	_, resp := envFire(t, router, http.MethodPost, "/api/maintenance/cleanup", nil)

	filesField, ok := resp["files"].(map[string]any)
	if !ok {
		t.Fatalf("files field missing or wrong type: %v", resp["files"])
	}
	if filesField["status"] != "ok" {
		t.Errorf("files.status = %q; want ok", filesField["status"])
	}
}

func TestMaintenanceHandler_Cleanup_TokensField(t *testing.T) {
	e := newTestEnv(t)
	router := maintenanceRouter(e)

	_, resp := envFire(t, router, http.MethodPost, "/api/maintenance/cleanup", nil)

	tokensField, ok := resp["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("tokens field missing or wrong type: %v", resp["tokens"])
	}
	if tokensField["revocations_deleted"] == nil {
		t.Error("expected revocations_deleted field in tokens")
	}
}

func TestMaintenanceHandler_Cleanup_ZeroRevocationsOnFreshEnv(t *testing.T) {
	e := newTestEnv(t)
	router := maintenanceRouter(e)

	_, resp := envFire(t, router, http.MethodPost, "/api/maintenance/cleanup", nil)

	tokensField, _ := resp["tokens"].(map[string]any)
	revocations, _ := tokensField["revocations_deleted"].(float64)
	if int(revocations) != 0 {
		t.Errorf("revocations_deleted = %v; want 0 (no expired revocations in fresh env)", revocations)
	}
}

func TestMaintenanceHandler_Cleanup_Idempotent(t *testing.T) {
	e := newTestEnv(t)
	router := maintenanceRouter(e)

	// Calling cleanup twice should both succeed.
	code1, _ := envFire(t, router, http.MethodPost, "/api/maintenance/cleanup", nil)
	code2, _ := envFire(t, router, http.MethodPost, "/api/maintenance/cleanup", nil)
	if code1 != http.StatusOK || code2 != http.StatusOK {
		t.Errorf("cleanup not idempotent: got %d, %d", code1, code2)
	}
}

func TestMaintenanceHandler_Cleanup_RequiresAdmin(t *testing.T) {
	e := newTestEnv(t)
	h := handler.NewMaintenanceHandler(e.cfg, e.registry, e.storage)
	r := noAuthRouter(func(r chi.Router) {
		r.Use(handler.RequireAdmin)
		r.Post("/api/maintenance/cleanup", h.Cleanup)
	})
	code, _ := envFire(t, r, http.MethodPost, "/api/maintenance/cleanup", nil)
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 (admin required)", code)
	}
}
