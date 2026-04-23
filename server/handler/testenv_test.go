// Package handler_test — testenv_test.go provides shared test infrastructure
// used by all handler test files in this package.
package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/0xdps/mesahub-core/auth"
	"github.com/0xdps/mesahub-core/cache"
	"github.com/0xdps/mesahub-core/config"
	"github.com/0xdps/mesahub-core/db"
	"github.com/0xdps/mesahub-core/files"
	"github.com/0xdps/mesahub-core/queue"
	"github.com/0xdps/mesahub-core/telemetry"
)

// testEnv holds all shared dependencies for handler integration tests.
type testEnv struct {
	dir      string
	pool     *db.Pool
	registry *db.Registry
	storage  *files.Storage
	queue    *queue.Queue
	tel      *telemetry.Counters
	cache    cache.Client
	cfg      *config.Config
}

// newTestEnv creates a fully isolated test environment backed by a temp dir.
// All resources are cleaned up via t.Cleanup.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()

	pool := db.NewPool(dir)
	reg, err := db.OpenRegistry(dir)
	if err != nil {
		t.Fatalf("OpenRegistry: %v", err)
	}
	storage, err := files.NewStorage(dir, &config.Config{})
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	wq := queue.New(16)
	tel := telemetry.New()
	c := cache.NewNoop()
	cfg := &config.Config{
		AdminToken:        "test-admin-token",
		SessionSecret:     "test-session-secret-32byteslong!!",
		DataPath:          dir,
		MaxSQLLength:      1_000_000,
		MaxSQLBindings:    5000,
		MaxVolumeUsagePct: 95, // high so we don't trip the storage guard in tests
	}

	t.Cleanup(func() {
		wq.Stop()
		pool.Close()
		_ = reg.Close()
		_ = storage.Close()
	})

	return &testEnv{
		dir:      dir,
		pool:     pool,
		registry: reg,
		storage:  storage,
		queue:    wq,
		tel:      tel,
		cache:    c,
		cfg:      cfg,
	}
}

// adminRouter returns a chi router that automatically stamps the admin session
// header on every request — simulating a valid Bearer token going through
// AdminStamper. Handlers are wired by the caller.
func adminRouter(e *testEnv, mount func(r chi.Router)) *chi.Mux {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req.Header.Set(auth.AdminSessionHeader, "1")
			next.ServeHTTP(w, req)
		})
	})
	mount(r)
	return r
}

// noAuthRouter is like adminRouter but does NOT inject the admin header,
// so RequireAdmin middleware will reject requests.
func noAuthRouter(mount func(r chi.Router)) *chi.Mux {
	r := chi.NewRouter()
	mount(r)
	return r
}

// envFire fires a JSON request against a router and decodes the response.
// It returns the status code and the decoded JSON body (map or slice).
func envFire(t *testing.T, router http.Handler, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	return rr.Code, resp
}

// envFireRaw is like envFire but returns the raw response body as string
// (useful when response is a JSON array).
func envFireRaw(t *testing.T, router http.Handler, method, path string, body any) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}
