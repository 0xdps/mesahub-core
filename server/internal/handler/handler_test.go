package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0xdps/sqlite-hub/server/internal/handler"
)

// ── GET /api/health ───────────────────────────────────────────────────────────

func TestHealth_Status(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	handler.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rr.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %q; want ok", resp["status"])
	}
}

func TestHealth_UptimePresent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	handler.Health(rr, req)

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["uptime_sec"] == nil {
		t.Error("expected uptime_sec field")
	}
}

func TestHealth_GoVersionPresent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	handler.Health(rr, req)

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp)
	ver, _ := resp["go_version"].(string)
	if !strings.HasPrefix(ver, "go") {
		t.Errorf("go_version = %q; expected go prefix", ver)
	}
}

// ── GET /api/version ──────────────────────────────────────────────────────────

func TestVersion_Fields(t *testing.T) {
	h := handler.NewVersion("1.2.3", "standalone", false)
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["version"] != "1.2.3" {
		t.Errorf("version = %q; want 1.2.3", resp["version"])
	}
	if resp["mode"] != "standalone" {
		t.Errorf("mode = %q; want standalone", resp["mode"])
	}
	if resp["redis"] != false {
		t.Errorf("redis = %v; want false", resp["redis"])
	}
}

func TestVersion_RedisTrue(t *testing.T) {
	h := handler.NewVersion("0.0.1", "control", true)
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rr := httptest.NewRecorder()
	h(rr, req)

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["redis"] != true {
		t.Errorf("redis = %v; want true", resp["redis"])
	}
	if resp["mode"] != "control" {
		t.Errorf("mode = %q; want control", resp["mode"])
	}
}

func TestVersion_ContentType(t *testing.T) {
	h := handler.NewVersion("1.0.0", "standalone", false)
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q; want application/json", ct)
	}
}
