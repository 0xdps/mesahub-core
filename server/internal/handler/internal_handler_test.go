package handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/0xdps/sqlite-hub/server/internal/auth"
	"github.com/0xdps/sqlite-hub/server/internal/handler"
)

// internalRouter wires InternalHandler — no auth middleware (control plane
// routes are protected by RequireControlPlane, but for internal tests we want
// direct access to test the handler logic itself).
func internalRouter(e *testEnv) *chi.Mux {
	h := handler.NewInternalHandler(e.cache)
	r := chi.NewRouter()
	// Stamp control-plane header so RequireControlPlane passes.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req.Header.Set(auth.ControlPlaneHeader, "1")
			next.ServeHTTP(w, req)
		})
	})
	r.Use(auth.RequireControlPlane)
	r.Delete("/api/internal/cache/apikey/{hash}", h.InvalidateAPIKey)
	return r
}

// ── DELETE /api/internal/cache/apikey/:hash ───────────────────────────────────

func TestInternalHandler_InvalidateAPIKey_OK(t *testing.T) {
	e := newTestEnv(t)
	router := internalRouter(e)

	// A valid 64-char hex SHA-256 hash.
	hash := strings.Repeat("a", 64)
	code, resp := envFire(t, router, http.MethodDelete, "/api/internal/cache/apikey/"+hash, nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d; want 200 — body: %v", code, resp)
	}
	if resp["evicted"] != true {
		t.Errorf("evicted = %v; want true", resp["evicted"])
	}
}

func TestInternalHandler_InvalidateAPIKey_ShortHash(t *testing.T) {
	e := newTestEnv(t)
	router := internalRouter(e)

	// Hash shorter than 8 chars — should be rejected.
	code, _ := envFire(t, router, http.MethodDelete, "/api/internal/cache/apikey/abc", nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 (hash too short)", code)
	}
}

func TestInternalHandler_InvalidateAPIKey_ExactlyMin(t *testing.T) {
	e := newTestEnv(t)
	router := internalRouter(e)

	// 8 chars exactly — at the threshold, should pass the length check.
	hash := strings.Repeat("b", 8)
	code, resp := envFire(t, router, http.MethodDelete, "/api/internal/cache/apikey/"+hash, nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d; want 200 — body: %v", code, resp)
	}
	if resp["evicted"] != true {
		t.Errorf("evicted = %v; want true", resp["evicted"])
	}
}

func TestInternalHandler_InvalidateAPIKey_SevenChars_Rejected(t *testing.T) {
	e := newTestEnv(t)
	router := internalRouter(e)

	// 7 chars — one below the minimum.
	hash := strings.Repeat("c", 7)
	code, _ := envFire(t, router, http.MethodDelete, "/api/internal/cache/apikey/"+hash, nil)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 (7-char hash is too short)", code)
	}
}

func TestInternalHandler_InvalidateAPIKey_NoControlPlaneHeader(t *testing.T) {
	e := newTestEnv(t)

	// Router without control-plane stamp.
	h := handler.NewInternalHandler(e.cache)
	r := chi.NewRouter()
	r.Use(auth.RequireControlPlane)
	r.Delete("/api/internal/cache/apikey/{hash}", h.InvalidateAPIKey)

	hash := strings.Repeat("d", 64)
	code, _ := envFire(t, r, http.MethodDelete, "/api/internal/cache/apikey/"+hash, nil)
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 (no control-plane header)", code)
	}
}
