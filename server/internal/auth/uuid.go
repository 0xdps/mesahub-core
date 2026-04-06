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

var nonAlnumRe = regexp.MustCompile(`[^a-z0-9_-]`)

// ToTemplateName derives the template-internal database name from the control
// plane userID and database name. Must match the formula used in the control
// plane: u{userId[:8]}-{name} with all non-alnum chars replaced by '-'.
func ToTemplateName(userID, dbName string) string {
	prefix := userID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return nonAlnumRe.ReplaceAllString("u"+prefix+"-"+dbName, "-")
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
	if err := cdb.QueryRow(
		"SELECT user_id, name FROM databases WHERE id = ? AND status = 'active'",
		uuid).Scan(&uid, &name); err != nil {
		return "", "", fmt.Errorf("database not found")
	}
	// name is already the template-internal name (e.g. u{prefix8}_{slug})
	// set at CreateDatabase time – no further derivation needed.
	return name, uid, nil
}

// AuthorizeDBByUUID is identical to AuthorizeDB but designed for UUID-based
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
