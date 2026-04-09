// Package auth — authorize.go contains DB-level access authorization helpers,
// mirroring authorizeDbRequest / timingSafeMatch from auth.ts.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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
// last_used_at, and populates the cache.
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

	// Join users to ensure the account is still active (plan not expired/revoked).
	var keyID, userID, scopesJSON string
	if err := cdb.QueryRow(`
		SELECT k.id, k.user_id, k.scopes
		FROM api_keys k
		JOIN users u ON u.id = k.user_id
   WHERE k.key_hash = ? AND k.status = 'active' AND u.status = 'active'`,
		keyHash).Scan(&keyID, &userID, &scopesJSON); err != nil {
		return nil, false
	}

	var scopes []string
	if err := json.Unmarshal([]byte(scopesJSON), &scopes); err != nil {
		return nil, false
	}

	// Stamp last_used_at — best-effort; open a separate writable conn.
	if wdb, err := sql.Open("sqlite3", controlPath); err == nil {
		_, _ = wdb.Exec("UPDATE api_keys SET last_used_at = datetime('now') WHERE id = ?", keyID)
		_ = wdb.Close()
	}

	kv := &cache.APIKeyValue{
		UserID: userID,
		KeyID:  keyID,
		Scopes: scopes,
	}
	_ = c.SetAPIKey(ctx, keyHash, *kv, apiKeyTTL)
	return kv, true
}

// hasPermission reports whether any of the given scopes grants the requested
// operation on the specified resource.
//
// resourceType is "db" or "bucket"; slug is the template-internal identifier;
// op is "read" or "write". A ":w" scope satisfies both read and write.
func hasPermission(scopes []string, resourceType, slug, op string) bool {
	for _, s := range scopes {
		parts := strings.SplitN(s, ":", 3)
		switch len(parts) {
		case 2:
			// "all:r" or "all:w"
			if parts[0] == "all" {
				if parts[1] == "w" {
					return true
				}
				if parts[1] == "r" && op == "read" {
					return true
				}
			}
		case 3:
			rtype, target, level := parts[0], parts[1], parts[2]
			if rtype != resourceType {
				continue
			}
			// Wildcard or exact slug match
			if target != "*" && target != slug {
				continue
			}
			if level == "w" {
				return true // write implies read
			}
			if level == "r" && op == "read" {
				return true
			}
		}
	}
	return false
}

// AuthorizeDB enforces the per-DB access rules:
//
//  1. X-Sqlite-Hub-Admin: 1 (stamped by AdminStamper) → allowed
//  2. DB inactive → 503
//  3. shs_ API key: owner match + scope enforcement via hasPermission
//     op is "read" for /query routes, "write" for /exec routes
//  4. Otherwise → 401
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
		op := "read"
		if strings.Contains(r.URL.Path, "/exec") {
			op = "write"
		}
		if !hasPermission(kv.Scopes, "db", record.Name, op) {
			return http.StatusForbidden, "This API key does not have access to this database"
		}
		return 0, ""
	}

	return http.StatusUnauthorized, "Unauthorized"
}

// LookupBucket looks up a bucket's owner by its slug from control.db.
// Returns userID and true on success.
func LookupBucket(dataPath, slug string) (string, bool) {
	controlPath := filepath.Join(dataPath, "control.db")
	if _, err := os.Stat(controlPath); os.IsNotExist(err) {
		return "", false
	}
	cdb, err := sql.Open("sqlite3", controlPath+"?mode=ro")
	if err != nil {
		return "", false
	}
	defer cdb.Close()
	var userID string
	if err := cdb.QueryRow(
		`SELECT user_id FROM buckets WHERE slug = ? AND status = 'active'`, slug,
	).Scan(&userID); err != nil {
		return "", false
	}
	return userID, true
}

// AuthorizeBucket enforces per-bucket access rules:
//
//  1. X-Sqlite-Hub-Admin: 1 (stamped by AdminStamper) → allowed
//  2. shs_ API key: owner match + scope enforcement via hasPermission
//     op is "read" for GET/HEAD, "write" for POST/DELETE/PATCH
//  3. Otherwise → 401
//
// Returns HTTP status 0 and "" when access is granted.
func AuthorizeBucket(r *http.Request, cfg *config.Config, c cache.Client, bucketUserID, bucketSlug string) (int, string) {
	if r.Header.Get(AdminSessionHeader) == "1" {
		return 0, ""
	}

	bearer := extractBearer(r)
	if bearer != "" && strings.HasPrefix(bearer, "shs_") {
		kv, ok := ValidateAPIKey(r.Context(), c, cfg.DataPath, bearer)
		if !ok || kv.UserID != bucketUserID {
			return http.StatusUnauthorized, "Unauthorized"
		}
		op := "read"
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			op = "write"
		}
		if !hasPermission(kv.Scopes, "bucket", bucketSlug, op) {
			return http.StatusForbidden, "This API key does not have access to this bucket"
		}
		return 0, ""
	}

	return http.StatusUnauthorized, "Unauthorized"
}

// UpdateBucketSizeInControl writes the current total file size for a bucket
// into control.db. Called asynchronously after file uploads and deletes.
// bucketSlug is the template-internal slug stored in buckets.slug.
func UpdateBucketSizeInControl(dataPath, bucketSlug string, sizeBytes int64) {
	controlPath := filepath.Join(dataPath, "control.db")
	cdb, err := sql.Open("sqlite3", controlPath)
	if err != nil {
		return
	}
	defer cdb.Close()
	_, _ = cdb.Exec(
		"UPDATE buckets SET size_bytes = ?, updated_at = datetime('now') WHERE slug = ?",
		sizeBytes, bucketSlug,
	)
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
