// Package auth — authorize.go contains DB-level access authorization helpers,
// mirroring authorizeDbRequest / timingSafeMatch from auth.ts.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
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
//  5. Otherwise → 401 or 403
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
