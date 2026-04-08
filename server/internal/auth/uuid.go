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
	nonAlnumRe = regexp.MustCompile(`[^a-z0-9_-]`)
	// uuidRE matches a standard hyphenated UUID (case-insensitive).
	uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// ToTemplateName derives the template-internal database name from the control
// plane userID and database name. Matches buildTemplateDbName in template-names.ts:
//
//	D-{userId with USR0 stripped, lowercased}-{dbName lowercased}
//
// with any non-alnum/dash/underscore chars replaced by '-'.
// Used as a fallback for rows that pre-date the template_name column.
func ToTemplateName(userID, dbName string) string {
	userPart := strings.ToLower(strings.TrimPrefix(strings.ToLower(userID), "usr0"))
	resName := strings.ToLower(dbName)
	raw := "D-" + userPart + "-" + resName
	return nonAlnumRe.ReplaceAllString(raw, "-")
}

// LookupByUUID resolves a control-plane database UUID to its template-internal
// name and owner user ID. Opens control.db read-only; returns an error if the
// database is not found or not active.
func LookupByUUID(dataPath, uuid string) (templateName, userID string, err error) {
	controlPath := filepath.Join(dataPath, "control.db")
	if _, statErr := os.Stat(controlPath); os.IsNotExist(statErr) {
		return "", "", fmt.Errorf("control.db not found")
	}
	cdb, err := sql.Open("sqlite3", controlPath+"?mode=ro")
	if err != nil {
		return "", "", err
	}
	defer cdb.Close()

	var uid, name string
	var tname sql.NullString
	if err := cdb.QueryRow(
		"SELECT user_id, name, template_name FROM databases WHERE id = ? AND status = 'active'",
		uuid).Scan(&uid, &name, &tname); err != nil {
		return "", "", fmt.Errorf("database not found")
	}
	// Use explicit template_name when present (set at CreateDatabase time).
	// Fall back to ToTemplateName derivation for rows created before the migration.
	if tname.Valid && tname.String != "" {
		return tname.String, uid, nil
	}
	return ToTemplateName(uid, name), uid, nil
}

// LookupByTemplateName resolves a template-internal database name (e.g.
// D-a6fb1cmq9-mydb) to its owner user ID and control-plane UUID.
// Returns an error if the database is not found or not active.
func LookupByTemplateName(dataPath, templateName string) (userID, uuid string, err error) {
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
		"SELECT user_id, id FROM databases WHERE template_name = ? AND status = 'active'",
		templateName).Scan(&uid, &id); err != nil {
		return "", "", fmt.Errorf("database not found")
	}
	return uid, id, nil
}

// ResolveDB resolves a database reference — either a control-plane UUID or a
// template-internal name (e.g. D-a6fb1cmq9-mydb) — to the template name,
// owner user ID, and the control-plane UUID needed for AuthorizeDBByUUID.
//
// Clients can now address databases two ways:
//
//	POST /query/aa15ab54-1bd4-4e0a-9abb-4c09361fc444   (UUID)
//	POST /query/D-a6fb1cmq9-test-db                    (template name)
func ResolveDB(dataPath, ref string) (templateName, userID, resolvedUUID string, err error) {
	if uuidRE.MatchString(ref) {
		tname, uid, e := LookupByUUID(dataPath, ref)
		return tname, uid, ref, e
	}
	// Treat ref as a template-internal name.
	uid, id, e := LookupByTemplateName(dataPath, ref)
	return ref, uid, id, e
}


// routes: when enforcing the scope of a "databases"-scoped API key, it checks
// did == uuid (the URL parameter) rather than did == record.Name (the template
// name). This is correct because api_key_databases.database_id stores the
// control-plane UUID, not the template-internal name.
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
		// Scope enforcement using the new string format.
		// db:* or all → access any owned DB; db:<name> checked against template name.
		// For UUID routes we also accept db:<name> where name is the user-visible name.
		switch {
		case kv.Scope == "all" || kv.Scope == "db:*":
			return 0, ""
		case strings.HasPrefix(kv.Scope, "db:"):
			scopedName := kv.Scope[3:]
			// Match against template-internal name (stored in record.Name)
			if scopedName == record.Name {
				return 0, ""
			}
			return http.StatusForbidden, "This API key does not have access to this database"
		default:
			return http.StatusForbidden, "This API key does not have access to this database"
		}
	}

	return http.StatusUnauthorized, "Unauthorized"
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
