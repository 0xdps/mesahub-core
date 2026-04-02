// Package handler contains HTTP handlers wired to chi routes.
package handler

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

var startTime = time.Now()

// HealthResponse is the JSON body returned by GET /api/health.
type HealthResponse struct {
	Status    string  `json:"status"`
	UptimeSec float64 `json:"uptime_sec"`
	GoVersion string  `json:"go_version"`
}

// Health handles GET /api/health.
func Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:    "ok",
		UptimeSec: time.Since(startTime).Seconds(),
		GoVersion: runtime.Version(),
	})
}

// VersionResponse is the JSON body returned by GET /api/version.
type VersionResponse struct {
	Version string `json:"version"`
	Mode    string `json:"mode"`
	Redis   bool   `json:"redis"`
}

// NewVersion returns a handler for GET /api/version.
// mode and redisAvailable are injected at startup.
func NewVersion(version, mode string, redisAvailable bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, VersionResponse{
			Version: version,
			Mode:    mode,
			Redis:   redisAvailable,
		})
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ErrorJSON writes a standard {"error": msg} response.
func ErrorJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
