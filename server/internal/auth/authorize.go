// Package auth — authorize.go contains DB-level access authorization helpers,
// mirroring authorizeDbRequest / isInternalRequest / timingSafeMatch from auth.ts.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"github.com/0xdps/sqlite-hub/server/internal/config"
	"github.com/0xdps/sqlite-hub/server/internal/db"
)

// AdminSessionHeader is set by AdminStamper after auth verification and is
// stripped from every incoming request by StripInternalHeaders so it cannot
// be forged externally.
const AdminSessionHeader = "X-Sqlite-Hub-Admin"

// TimingSafeMatch compares a and b in constant time to prevent timing-oracle
// attacks. Returns false if either string is empty.
func TimingSafeMatch(a, b string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	bufA := make([]byte, maxLen)
	bufB := make([]byte, maxLen)
	copy(bufA, a)
	copy(bufB, b)
	return subtle.ConstantTimeCompare(bufA, bufB) == 1
}

// IsInternalRequest returns true when the inferred client IP is a private /
// loopback address (RFC1918, ::1, Railway ULA prefix fd::/8).
func IsInternalRequest(r *http.Request) bool {
	return isPrivateIP(clientIP(r))
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0])
	}
	if ri := r.Header.Get("X-Real-IP"); ri != "" {
		return strings.TrimSpace(ri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isPrivateIP(ip string) bool {
	ip = strings.TrimPrefix(ip, "::ffff:")
	if ip == "127.0.0.1" || ip == "::1" || ip == "localhost" {
		return true
	}
	// Railway / Docker private network: ULA addresses start with fd
	if len(ip) >= 3 && strings.EqualFold(ip[:2], "fd") {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(parsed) {
			return true
		}
	}
	return false
}

// ValidateAPIKey looks up an shs_ API key in control.db (control-mode only).
// Returns (userID, true) on success, ("", false) if the key is unknown or the
// control database does not exist.
func ValidateAPIKey(dataPath, keyValue string) (string, bool) {
	controlPath := filepath.Join(dataPath, "control.db")
	if _, err := os.Stat(controlPath); os.IsNotExist(err) {
		return "", false
	}
	cdb, err := sql.Open("sqlite3", controlPath+"?mode=ro")
	if err != nil {
		return "", false
	}
	defer cdb.Close()

	sum := sha256.Sum256([]byte(keyValue))
	keyHash := hex.EncodeToString(sum[:])

	var id, userID string
	if err := cdb.QueryRow(
		"SELECT id, user_id FROM api_keys WHERE key_hash = ? AND status = 'active'",
		keyHash).Scan(&id, &userID); err != nil {
		return "", false
	}
	// Stamp last_used_at — best-effort; open a separate writable conn.
	if wdb, err := sql.Open("sqlite3", controlPath); err == nil {
		_, _ = wdb.Exec("UPDATE api_keys SET last_used_at = datetime('now') WHERE id = ?", id)
		_ = wdb.Close()
	}
	return userID, true
}

// AuthorizeDB enforces the per-DB access rules from auth.ts:
//
//  1. X-Sqlite-Hub-Admin: 1 (stamped by AdminStamper) → allowed
//  2. DB inactive → 503
//  3. shs_ API key matching owner → allowed
//  4. Per-DB service_secret Bearer match → allowed
//  5. No service_secret + internal IP (opt-in) → allowed
//  6. Otherwise → 401 or 403
//
// Returns HTTP status 0 and "" when access is granted.
func AuthorizeDB(r *http.Request, cfg *config.Config, record *db.DBRecord) (int, string) {
	if r.Header.Get(AdminSessionHeader) == "1" {
		return 0, ""
	}
	if record.Status != "active" {
		return http.StatusServiceUnavailable, "This database is inactive"
	}

	bearer := extractBearer(r)

	if bearer != "" && strings.HasPrefix(bearer, "shs_") {
		userID, ok := ValidateAPIKey(cfg.DataPath, bearer)
		if ok && record.Owner == userID {
			return 0, ""
		}
		return http.StatusUnauthorized, "Unauthorized"
	}

	if record.ServiceSecret.Valid && record.ServiceSecret.String != "" {
		if bearer != "" && TimingSafeMatch(bearer, record.ServiceSecret.String) {
			return 0, ""
		}
		return http.StatusUnauthorized, "Unauthorized"
	}

	if strings.EqualFold(os.Getenv("ALLOW_INTERNAL_DB_ACCESS_WITHOUT_SECRET"), "true") &&
		IsInternalRequest(r) {
		return 0, ""
	}
	return http.StatusForbidden,
		"Forbidden: this database has no service_secret — set a service_secret for API access"
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
