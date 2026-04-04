// Package auth — authorize.go contains DB-level access authorization helpers,
// mirroring authorizeDbRequest / timingSafeMatch from auth.ts.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/0xdps/sqlite-hub/server/internal/cache"
	"github.com/0xdps/sqlite-hub/server/internal/config"
	"github.com/0xdps/sqlite-hub/server/internal/db"
)

// apiKeyTTL is how long a validated API key result is cached.
// After control revokes a key it calls DELETE /api/internal/cache/apikey/{hash}
// to instantly evict it, so this TTL is now purely a safety net for cache
// backend failures — not the primary revocation mechanism.
const apiKeyTTL = 15 * time.Minute

// AdminSessionHeader is set by AdminStamper after auth verification and is
// stripped from every incoming request by StripInternalHeaders so it cannot
// be forged externally.
const AdminSessionHeader = "X-Sqlite-Hub-Admin"

// ControlPlaneHeader is set by RequireControlPlane after verifying
// CONTROL_PLANE_SECRET. Stripped on ingress so it cannot be forged.
const ControlPlaneHeader = "X-Sqlite-Hub-Control"

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
// It checks the cache first; on a miss it queries control.db, stamps
// last_used_at, reads scope + permitted database IDs, and populates the cache.
// Returns a populated APIKeyValue and true on success, nil and false otherwise.
func ValidateAPIKey(ctx context.Context, c cache.Client, dataPath, keyValue string) (*cache.APIKeyValue, bool) {
	sum := sha256.Sum256([]byte(keyValue))
	keyHash := hex.EncodeToString(sum[:])

	// ── Cache hit ─────────────────────────────────────────────────────────────
	if cached, err := c.GetAPIKey(ctx, keyHash); err == nil && cached != nil {
		return cached, true
	}

	// ── Cache miss: query control.db ──────────────────────────────────────────
	controlPath := filepath.Join(dataPath, "control.db")
	if _, err := os.Stat(controlPath); os.IsNotExist(err) {
		return nil, false
	}
	cdb, err := sql.Open("sqlite3", controlPath+"?mode=ro")
	if err != nil {
		return nil, false
	}
	defer cdb.Close()

	var keyID, userID, scope string
	if err := cdb.QueryRow(
		"SELECT id, user_id, scope FROM api_keys WHERE key_hash = ? AND status = 'active'",
		keyHash).Scan(&keyID, &userID, &scope); err != nil {
		return nil, false
	}

	// Stamp last_used_at — best-effort; open a separate writable conn.
	if wdb, err := sql.Open("sqlite3", controlPath); err == nil {
		_, _ = wdb.Exec("UPDATE api_keys SET last_used_at = datetime('now') WHERE id = ?", keyID)
		_ = wdb.Close()
	}

	// Read permitted database IDs for scoped keys.
	var dbIDs []string
	if scope == "databases" {
		rows, err := cdb.Query(
			"SELECT database_id FROM api_key_databases WHERE api_key_id = ?", keyID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var did string
				if rows.Scan(&did) == nil {
					dbIDs = append(dbIDs, did)
				}
			}
		}
	}

	kv := &cache.APIKeyValue{
		UserID:      userID,
		KeyID:       keyID,
		Scope:       scope,
		DatabaseIDs: dbIDs,
	}
	_ = c.SetAPIKey(ctx, keyHash, *kv, apiKeyTTL)
	return kv, true
}

// AuthorizeDB enforces the per-DB access rules:
//
//  1. X-Sqlite-Hub-Admin: 1 (stamped by AdminStamper) → allowed
//  2. DB inactive → 503
//  3. shs_ API key: owner match + scope enforcement (account or scoped to this DB)
//  4. Per-DB service_secret Bearer match → allowed
//  5. Otherwise → 401 or 403
//
// Returns HTTP status 0 and "" when access is granted.
func AuthorizeDB(r *http.Request, cfg *config.Config, c cache.Client, record *db.DBRecord) (int, string) {
	if r.Header.Get(AdminSessionHeader) == "1" {
		return 0, ""
	}
	if record.Status != "active" {
		return http.StatusServiceUnavailable, "This database is inactive"
	}

	bearer := extractBearer(r)

	if bearer != "" && strings.HasPrefix(bearer, "shs_") {
		kv, ok := ValidateAPIKey(r.Context(), c, cfg.DataPath, bearer)
		if !ok || kv.UserID != record.Owner {
			return http.StatusUnauthorized, "Unauthorized"
		}
		// Scope enforcement: "account" keys can access any owned DB;
		// "databases" keys are restricted to their explicit allow-list.
		if kv.Scope == "databases" {
			allowed := false
			for _, did := range kv.DatabaseIDs {
				if did == record.Name {
					allowed = true
					break
				}
			}
			if !allowed {
				return http.StatusForbidden, "This API key does not have access to this database"
			}
		}
		return 0, ""
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
