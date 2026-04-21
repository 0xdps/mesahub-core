// Package auth — uuid.go contains helpers for control-mode UUID-based DB routes.
// In control mode, clients address databases by their UUID (from the control
// plane's databases table) rather than by the template-internal name.
package auth

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"github.com/0xdps/sqlite-hub/server/internal/cache"
	"github.com/0xdps/sqlite-hub/server/internal/config"
	"github.com/0xdps/sqlite-hub/server/internal/db"
)

var (
	// nonAlnumRe matches chars not kept by sanitizeInternalName in template-names.ts:
	// /[^A-Za-z0-9_-]/g
	nonAlnumRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)
	// uuidRE matches a standard hyphenated UUID (case-insensitive).
	uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// ToTemplateName derives the template-internal database name from the control
// plane userID and database name. Matches buildTemplateDbName in template-names.ts:
//
//	{userId with USR0 stripped, lowercased}-{dbName lowercased}
//
// with any non-alnum/dash/underscore chars replaced by '-'.
// Used as a fallback for rows that pre-date the slug column.
func ToTemplateName(userID, dbName string) string {
	userPart := strings.ToLower(strings.TrimPrefix(strings.ToLower(userID), "usr0"))
	resName := strings.ToLower(dbName)
	raw := userPart + "-" + resName
	return nonAlnumRe.ReplaceAllString(raw, "-")
}

// LookupByUUID resolves a control-plane database UUID to its template-internal
// slug and owner user ID. Opens control.db read-only; returns an error if the
// database is not found or not active.
func LookupByUUID(dataPath, uuid string) (slug, userID string, err error) {
	controlPath := filepath.Join(dataPath, "control.db")
	if _, statErr := os.Stat(controlPath); os.IsNotExist(statErr) {
		return "", "", fmt.Errorf("control.db not found")
	}
	cdb, err := sql.Open("sqlite3", controlPath+"?mode=ro")
	if err != nil {
		return "", "", err
	}
	defer cdb.Close()

	var uid, dbSlug string
	if err := cdb.QueryRow(
		"SELECT user_id, slug FROM databases WHERE id = ? AND status = 'active'",
		uuid).Scan(&uid, &dbSlug); err != nil {
		return "", "", fmt.Errorf("database not found")
	}
	return dbSlug, uid, nil
}

// LookupBySlug resolves a template-internal database slug (e.g.
// D-a6fb1cmq9-mydb) to its owner user ID and control-plane UUID.
// Returns an error if the database is not found or not active.
func LookupBySlug(dataPath, slug string) (userID, uuid string, err error) {
	controlPath := filepath.Join(dataPath, "control.db")
	if _, statErr := os.Stat(controlPath); os.IsNotExist(statErr) {
		return "", "", fmt.Errorf("control.db not found")
	}
	cdb, err := sql.Open("sqlite3", controlPath+"?mode=ro")
	if err != nil {
		return "", "", err
	}
	defer cdb.Close()

	var uid, id string
	if err := cdb.QueryRow(
		"SELECT user_id, id FROM databases WHERE slug = ? AND status = 'active'",
		slug).Scan(&uid, &id); err != nil {
		return "", "", fmt.Errorf("database not found")
	}
	return uid, id, nil
}

// ResolveDB resolves a database reference — either a control-plane UUID or a
// template-internal slug (e.g. D-a6fb1cmq9-mydb) — to the slug,
// owner user ID, and the control-plane UUID needed for AuthorizeDBByUUID.
//
// Clients can now address databases two ways:
//
//	POST /query/aa15ab54-1bd4-4e0a-9abb-4c09361fc444   (UUID)
//	POST /query/D-a6fb1cmq9-test-db                    (slug)
func ResolveDB(dataPath, ref string) (slug, userID, resolvedUUID string, err error) {
	if uuidRE.MatchString(ref) {
		s, uid, e := LookupByUUID(dataPath, ref)
		return s, uid, ref, e
	}
	// Treat ref as a template-internal slug.
	uid, id, e := LookupBySlug(dataPath, ref)
	return ref, uid, id, e
}

// routes: when enforcing the scope of a "databases"-scoped API key, it checks
// against record.Name (the template-internal slug) so the scope string
// ["db:D-abc-mydb:w"] matches the slug stored in the databases table.
//
// It also accepts a valid control-plane user session (sqlitedbhub_session cookie)
// when the logged-in user owns this database (verified via control.db).
func AuthorizeDBByUUID(r *http.Request, cfg *config.Config, c cache.Client, record *db.DBRecord, uuid string) (int, string) {
	if r.Header.Get(AdminSessionHeader) == "1" {
		return 0, ""
	}

	// Accept the control-plane user session when the user owns this database.
	if cookie, err := r.Cookie("sqlitedbhub_session"); err == nil && cookie.Value != "" {
		if sv, err := c.GetSession(r.Context(), cookie.Value); err == nil && sv != nil &&
			sv.Role == "user" && sv.UserID != "" {
			if IsOwnedByControlUser(cfg.DataPath, uuid, sv.UserID) {
				return 0, ""
			}
		}
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

// LookupBucketByUUID resolves a control-plane bucket UUID to its
// template-internal slug and owner user ID.
func LookupBucketByUUID(dataPath, uuid string) (slug, userID string, err error) {
	controlPath := filepath.Join(dataPath, "control.db")
	if _, statErr := os.Stat(controlPath); os.IsNotExist(statErr) {
		return "", "", fmt.Errorf("control.db not found")
	}
	cdb, err := sql.Open("sqlite3", controlPath+"?mode=ro")
	if err != nil {
		return "", "", err
	}
	defer cdb.Close()

	var uid, bslug string
	if err := cdb.QueryRow(
		"SELECT user_id, slug FROM buckets WHERE id = ? AND status = 'active'",
		uuid).Scan(&uid, &bslug); err != nil {
		return "", "", fmt.Errorf("bucket not found")
	}
	return bslug, uid, nil
}

// LookupBucketBySlug resolves a template-internal bucket slug (e.g.
// B-a6fb1cmq9-photos) to its owner user ID and control-plane UUID.
func LookupBucketBySlug(dataPath, slug string) (userID, uuid string, err error) {
	controlPath := filepath.Join(dataPath, "control.db")
	if _, statErr := os.Stat(controlPath); os.IsNotExist(statErr) {
		return "", "", fmt.Errorf("control.db not found")
	}
	cdb, err := sql.Open("sqlite3", controlPath+"?mode=ro")
	if err != nil {
		return "", "", err
	}
	defer cdb.Close()

	var uid, id string
	if err := cdb.QueryRow(
		"SELECT user_id, id FROM buckets WHERE slug = ? AND status = 'active'",
		slug).Scan(&uid, &id); err != nil {
		return "", "", fmt.Errorf("bucket not found")
	}
	return uid, id, nil
}

// ResolveBucket resolves a bucket reference — either a control-plane UUID or
// a template-internal slug — to the slug, owner user ID, and UUID.
//
// Clients can address buckets two ways:
//
//	GET /api/buckets/aa15ab54-1bd4-4e0a-9abb-4c09361fc444/files   (UUID)
//	GET /api/buckets/B-a6fb1cmq9-photos/files                     (slug)
func ResolveBucket(dataPath, ref string) (slug, userID, resolvedUUID string, err error) {
	if uuidRE.MatchString(ref) {
		s, uid, e := LookupBucketByUUID(dataPath, ref)
		return s, uid, ref, e
	}
	// Treat ref as a template-internal slug.
	uid, id, e := LookupBucketBySlug(dataPath, ref)
	return ref, uid, id, e
}

// IsOwnedByControlUser reports whether the database with the given UUID is
// owned by the control-plane user with the given userID. Opens control.db
// read-only; returns false on any error.
func IsOwnedByControlUser(dataPath, uuid, userID string) bool {
	controlPath := filepath.Join(dataPath, "control.db")
	if _, statErr := os.Stat(controlPath); os.IsNotExist(statErr) {
		return false
	}
	cdb, err := sql.Open("sqlite3", controlPath+"?mode=ro")
	if err != nil {
		return false
	}
	defer cdb.Close()
	var n int
	err = cdb.QueryRow(
		"SELECT COUNT(*) FROM databases WHERE id = ? AND user_id = ? AND status = 'active'",
		uuid, userID).Scan(&n)
	return err == nil && n > 0
}
