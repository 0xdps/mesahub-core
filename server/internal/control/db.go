// Package control manages the control.db SQLite database used in
// SQLITE_HUB_MODE=control deployments.  It owns the schema, provides typed
// query helpers, and exposes a ControlDB type that wraps *sql.DB.
package control

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ── Schema ────────────────────────────────────────────────────────────────────

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS users (
  id          TEXT PRIMARY KEY,
  email       TEXT NOT NULL UNIQUE,
  nube_plan   TEXT NOT NULL DEFAULT 'free',
  nube_status TEXT NOT NULL DEFAULT 'active',
  created_at  TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT
);

CREATE TABLE IF NOT EXISTS instances (
  id                      TEXT PRIMARY KEY,
  url                     TEXT NOT NULL UNIQUE,
  region                  TEXT,
  admin_token_encrypted   TEXT NOT NULL,
  max_databases           INTEGER NOT NULL DEFAULT 100,
  current_databases_count INTEGER NOT NULL DEFAULT 0,
  is_available            INTEGER NOT NULL DEFAULT 1,
  health_check_at         TEXT,
  created_at              TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS databases (
  id            TEXT PRIMARY KEY,
  user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,          -- user-visible unique name (slug)
  template_name TEXT,                   -- template-internal name (e.g. D-<userpart>-<slug>)
  display_name  TEXT NOT NULL,
  description   TEXT,
  instance_id   TEXT REFERENCES instances(id),
  status        TEXT NOT NULL DEFAULT 'active',
  size_bytes    INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at    TEXT,
  UNIQUE(user_id, name)
);

CREATE TABLE IF NOT EXISTS buckets (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,          -- template-internal unique name
  display_name TEXT NOT NULL,
  description  TEXT,
  instance_id  TEXT REFERENCES instances(id),
  status       TEXT NOT NULL DEFAULT 'active',
  size_bytes   INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at   TEXT,
  UNIQUE(user_id, name)
);

-- Unified API keys for both databases and buckets.
-- scope examples:
--   'all'            → all databases and buckets
--   'db:*'           → all databases
--   'bucket:*'       → all buckets
--   'db:mydb'        → specific database by name
--   'bucket:photos'  → specific bucket by name
CREATE TABLE IF NOT EXISTS api_keys (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  key_hash     TEXT NOT NULL UNIQUE,
  scope        TEXT NOT NULL DEFAULT 'all',
  status       TEXT NOT NULL DEFAULT 'active',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS usage (
  id                 TEXT PRIMARY KEY,
  user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  period_year        INTEGER NOT NULL,
  period_month       INTEGER NOT NULL,
  queries_executed   INTEGER NOT NULL DEFAULT 0,
  exec_executed      INTEGER NOT NULL DEFAULT 0,
  api_calls          INTEGER NOT NULL DEFAULT 0,
  storage_bytes      INTEGER NOT NULL DEFAULT 0,
  bucket_bytes       INTEGER NOT NULL DEFAULT 0,
  calls_success      INTEGER NOT NULL DEFAULT 0,
  calls_client_err   INTEGER NOT NULL DEFAULT 0,
  calls_server_err   INTEGER NOT NULL DEFAULT 0,
  UNIQUE(user_id, period_year, period_month)
);

CREATE INDEX IF NOT EXISTS idx_databases_user_id ON databases(user_id);
CREATE INDEX IF NOT EXISTS idx_buckets_user_id   ON buckets(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id  ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_hash     ON api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_usage_user_period ON usage(user_id, period_year, period_month);
`

// migrations adds columns / tables that may be absent in databases created by
// older versions of the schema. Each statement is run with IGNORE semantics via
// a separate Exec call — errors (e.g. duplicate column) are silently swallowed.
var migrations = []string{
	`ALTER TABLE users     ADD COLUMN updated_at    TEXT`,
	`ALTER TABLE databases ADD COLUMN description   TEXT`,
	`ALTER TABLE databases ADD COLUMN instance_id   TEXT REFERENCES instances(id)`,
	`ALTER TABLE databases ADD COLUMN updated_at    TEXT`,
	// Legacy database credential fields are no longer used; auth uses api_keys.
	// (SQLite cannot DROP COLUMN in older versions; we simply stop reading/writing it)
	// Copy legacy sqlite_hub_instance_id → instance_id for pre-migration rows
	`UPDATE databases SET instance_id = sqlite_hub_instance_id WHERE instance_id IS NULL AND sqlite_hub_instance_id IS NOT NULL`,
	`CREATE TABLE IF NOT EXISTS buckets (
	  id           TEXT PRIMARY KEY,
	  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	  name         TEXT NOT NULL,
	  display_name TEXT NOT NULL,
	  description  TEXT,
	  instance_id  TEXT REFERENCES instances(id),
	  status       TEXT NOT NULL DEFAULT 'active',
	  size_bytes   INTEGER NOT NULL DEFAULT 0,
	  created_at   TEXT NOT NULL DEFAULT (datetime('now')),
	  updated_at   TEXT,
	  UNIQUE(user_id, name)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_buckets_user_id ON buckets(user_id)`,
	`ALTER TABLE usage ADD COLUMN bucket_bytes INTEGER NOT NULL DEFAULT 0`,
	// Add template_name to databases: stores the template-internal name (e.g. D-<userpart>-<slug>).
	// Rows inserted before this migration will have template_name = NULL; LookupByUUID
	// falls back to derivation for those rows.
	`ALTER TABLE databases ADD COLUMN template_name TEXT`,
}

// ── DB wrapper ────────────────────────────────────────────────────────────────

// ControlDB wraps *sql.DB with typed helpers for the control plane schema.
type ControlDB struct {
	db *sql.DB
}

// Open opens (or creates) control.db at dataPath/control.db,
// runs the schema migration, and returns a ready ControlDB.
func Open(dataPath string) (*ControlDB, error) {
	path := filepath.Join(dataPath, "control.db")
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("control: open: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite single-writer
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("control: schema: %w", err)
	}
	// Run additive migrations. "duplicate column" errors are expected on
	// already-migrated databases and are silently skipped. Any other error
	// is fatal — it indicates a genuine schema inconsistency.
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			msg := err.Error()
			if !strings.Contains(msg, "duplicate column") &&
				!strings.Contains(msg, "already exists") {
				db.Close()
				return nil, fmt.Errorf("control: migration failed: %w", err)
			}
		}
	}
	return &ControlDB{db: db}, nil
}

// Close releases the underlying database connection.
func (c *ControlDB) Close() error { return c.db.Close() }

// ── Types ─────────────────────────────────────────────────────────────────────

// User is a control-plane user record.
type User struct {
	ID         string  `json:"id"`
	Email      string  `json:"email"`
	NubePlan   string  `json:"nube_plan"`
	NubeStatus string  `json:"nube_status"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  *string `json:"updated_at,omitempty"`
}

// Database is a control-plane database record.
type Database struct {
	ID          string  `json:"id"`
	UserID      string  `json:"user_id"`
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	Description *string `json:"description,omitempty"`
	InstanceID  *string `json:"instance_id,omitempty"`
	Status      string  `json:"status"`
	SizeBytes   int64   `json:"size_bytes"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   *string `json:"updated_at,omitempty"`
}

// Bucket is a control-plane file-bucket record.
type Bucket struct {
	ID          string  `json:"id"`
	UserID      string  `json:"user_id"`
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	Description *string `json:"description,omitempty"`
	InstanceID  *string `json:"instance_id,omitempty"`
	Status      string  `json:"status"`
	SizeBytes   int64   `json:"size_bytes"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   *string `json:"updated_at,omitempty"`
}

// Usage holds per-user monthly usage counters.
type Usage struct {
	ID              string `json:"id"`
	UserID          string `json:"user_id"`
	PeriodYear      int    `json:"period_year"`
	PeriodMonth     int    `json:"period_month"`
	QueriesExecuted int64  `json:"queries_executed"`
	ExecExecuted    int64  `json:"exec_executed"`
	APICalls        int64  `json:"api_calls"`
	StorageBytes    int64  `json:"storage_bytes"`
	BucketBytes     int64  `json:"bucket_bytes"`
	CallsSuccess    int64  `json:"calls_success"`
	CallsClientErr  int64  `json:"calls_client_err"`
	CallsServerErr  int64  `json:"calls_server_err"`
}

// APIKey is a control-plane API key record (key_hash is never exposed via JSON).
type APIKey struct {
	ID         string  `json:"id"`
	UserID     string  `json:"user_id"`
	Name       string  `json:"name"`
	Scope      string  `json:"scope"`
	Status     string  `json:"status"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// ── User ops ──────────────────────────────────────────────────────────────────

// UpsertUser inserts or updates the user identified by nubeID.
// Returns the stored user record.
func (c *ControlDB) UpsertUser(nubeID, email, plan, status string) (*User, error) {
	_, err := c.db.Exec(`
		INSERT INTO users (id, email, nube_plan, nube_status)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  email       = excluded.email,
		  nube_plan   = excluded.nube_plan,
		  nube_status = excluded.nube_status`,
		nubeID, email, plan, status)
	if err != nil {
		return nil, fmt.Errorf("control: upsert user: %w", err)
	}
	return c.GetUser(nubeID)
}

// GetUser fetches a user by ID.
func (c *ControlDB) GetUser(id string) (*User, error) {
	u := &User{}
	err := c.db.QueryRow(
		`SELECT id, email, nube_plan, nube_status, created_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.NubePlan, &u.NubeStatus, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// ── Database ops ──────────────────────────────────────────────────────────────

// ListDatabases returns all non-deleted databases for a user.
func (c *ControlDB) ListDatabases(userID string) ([]Database, error) {
	rows, err := c.db.Query(
		`SELECT id, user_id, name, display_name, description, instance_id, status, size_bytes, created_at, updated_at
		 FROM databases WHERE user_id = ? AND status != 'deleted'
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDatabases(rows)
}

// GetDatabase fetches a database by ID (any status).
func (c *ControlDB) GetDatabase(id string) (*Database, error) {
	return scanOneDatabase(c.db.QueryRow(
		`SELECT id, user_id, name, display_name, description, instance_id, status, size_bytes, created_at, updated_at
		 FROM databases WHERE id = ?`, id))
}

// GetDatabaseByName fetches a database record by its template-internal name.
func (c *ControlDB) GetDatabaseByName(name string) (*Database, error) {
	return scanOneDatabase(c.db.QueryRow(
		`SELECT id, user_id, name, display_name, description, instance_id, status, size_bytes, created_at, updated_at
		 FROM databases WHERE name = ? AND status != 'deleted'`, name))
}

// CountActiveDatabases returns the number of active databases for a user.
func (c *ControlDB) CountActiveDatabases(userID string) (int, error) {
	var n int
	err := c.db.QueryRow(
		`SELECT COUNT(*) FROM databases WHERE user_id = ? AND status = 'active'`, userID,
	).Scan(&n)
	return n, err
}

// CreateDatabase inserts a new database record.
func (c *ControlDB) CreateDatabase(userID, name, displayName string) (*Database, error) {
	id := newID()
	_, err := c.db.Exec(
		`INSERT INTO databases (id, user_id, name, display_name) VALUES (?, ?, ?, ?)`,
		id, userID, name, displayName)
	if err != nil {
		return nil, fmt.Errorf("control: create database: %w", err)
	}
	return c.GetDatabase(id)
}

// SoftDeleteDatabase marks a database as deleted.
func (c *ControlDB) SoftDeleteDatabase(id, userID string) error {
	res, err := c.db.Exec(
		`UPDATE databases SET status = 'deleted' WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("control: database not found or not owned by user")
	}
	return nil
}

// UpdateDatabaseSize sets the cached size_bytes for a database record.
func (c *ControlDB) UpdateDatabaseSize(name string, sizeBytes int64) error {
	_, err := c.db.Exec(
		`UPDATE databases SET size_bytes = ? WHERE name = ?`, sizeBytes, name)
	return err
}

// ── Bucket ops ────────────────────────────────────────────────────────────────

// ListBuckets returns all non-deleted buckets for a user.
func (c *ControlDB) ListBuckets(userID string) ([]Bucket, error) {
	rows, err := c.db.Query(
		`SELECT id, user_id, name, display_name, description, instance_id, status, size_bytes, created_at, updated_at
		 FROM buckets WHERE user_id = ? AND status != 'deleted'
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBuckets(rows)
}

// GetBucket fetches a bucket by ID.
func (c *ControlDB) GetBucket(id string) (*Bucket, error) {
	return scanOneBucket(c.db.QueryRow(
		`SELECT id, user_id, name, display_name, description, instance_id, status, size_bytes, created_at, updated_at
		 FROM buckets WHERE id = ?`, id))
}

// GetBucketByName fetches a bucket by its template-internal name.
func (c *ControlDB) GetBucketByName(name string) (*Bucket, error) {
	return scanOneBucket(c.db.QueryRow(
		`SELECT id, user_id, name, display_name, description, instance_id, status, size_bytes, created_at, updated_at
		 FROM buckets WHERE name = ? AND status != 'deleted'`, name))
}

// CountActiveBuckets returns the number of active buckets for a user.
func (c *ControlDB) CountActiveBuckets(userID string) (int, error) {
	var n int
	err := c.db.QueryRow(
		`SELECT COUNT(*) FROM buckets WHERE user_id = ? AND status = 'active'`, userID,
	).Scan(&n)
	return n, err
}

// CreateBucket inserts a new bucket record.
func (c *ControlDB) CreateBucket(userID, name, displayName string, instanceID *string) (*Bucket, error) {
	id := newID()
	_, err := c.db.Exec(
		`INSERT INTO buckets (id, user_id, name, display_name, instance_id) VALUES (?, ?, ?, ?, ?)`,
		id, userID, name, displayName, instanceID)
	if err != nil {
		return nil, fmt.Errorf("control: create bucket: %w", err)
	}
	return c.GetBucket(id)
}

// SoftDeleteBucket marks a bucket as deleted.
func (c *ControlDB) SoftDeleteBucket(id, userID string) error {
	res, err := c.db.Exec(
		`UPDATE buckets SET status = 'deleted', updated_at = datetime('now') WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("control: bucket not found or not owned by user")
	}
	return nil
}

// UpdateBucketSize sets the cached size_bytes for a bucket record.
func (c *ControlDB) UpdateBucketSize(name string, sizeBytes int64) error {
	_, err := c.db.Exec(
		`UPDATE buckets SET size_bytes = ? WHERE name = ?`, sizeBytes, name)
	return err
}

// ── API key ops ───────────────────────────────────────────────────────────────

// ListAPIKeys returns all active API keys for a user.
func (c *ControlDB) ListAPIKeys(userID string) ([]APIKey, error) {
	rows, err := c.db.Query(
		`SELECT id, user_id, name, scope, status, last_used_at, created_at
		 FROM api_keys WHERE user_id = ? AND status = 'active'
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAPIKeys(rows)
}

// CreateAPIKey generates a new API key and inserts it.
// scope examples: 'all', 'db:*', 'db:mydb', 'bucket:*', 'bucket:photos'
// Returns the created APIKey and the raw key value (shown once only).
func (c *ControlDB) CreateAPIKey(userID, name, scope string) (*APIKey, string, error) {
	rawKey := "shs_" + randomHex(32)
	sum := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(sum[:])

	id := newID()
	_, err := c.db.Exec(
		`INSERT INTO api_keys (id, user_id, name, key_hash, scope) VALUES (?, ?, ?, ?, ?)`,
		id, userID, name, keyHash, scope)
	if err != nil {
		return nil, "", fmt.Errorf("control: create api key: %w", err)
	}

	key, err := c.getAPIKey(id)
	if err != nil {
		return nil, "", err
	}
	return key, rawKey, nil
}

// RevokeAPIKey sets status = 'revoked' for a key owned by userID.
func (c *ControlDB) RevokeAPIKey(id, userID string) error {
	res, err := c.db.Exec(
		`UPDATE api_keys SET status = 'revoked' WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("control: api key not found or not owned by user")
	}
	return nil
}

// GetAPIKeyHash returns the key_hash for a given key ID (for cache invalidation).
func (c *ControlDB) GetAPIKeyHash(id string) (string, error) {
	var h string
	err := c.db.QueryRow(`SELECT key_hash FROM api_keys WHERE id = ?`, id).Scan(&h)
	return h, err
}

// ── Usage ops ─────────────────────────────────────────────────────────────────

// GetCurrentUsage returns (or creates) the usage row for the current calendar
// month for the given user.
func (c *ControlDB) GetCurrentUsage(userID string) (*Usage, error) {
	now := time.Now().UTC()
	year, month := now.Year(), int(now.Month())

	_, err := c.db.Exec(`
		INSERT OR IGNORE INTO usage (id, user_id, period_year, period_month)
		VALUES (?, ?, ?, ?)`, newID(), userID, year, month)
	if err != nil {
		return nil, fmt.Errorf("control: ensure usage row: %w", err)
	}

	u := &Usage{}
	err = c.db.QueryRow(`
		SELECT id, user_id, period_year, period_month,
		       queries_executed, exec_executed, api_calls, storage_bytes, bucket_bytes,
		       calls_success, calls_client_err, calls_server_err
		FROM usage WHERE user_id = ? AND period_year = ? AND period_month = ?`,
		userID, year, month,
	).Scan(&u.ID, &u.UserID, &u.PeriodYear, &u.PeriodMonth,
		&u.QueriesExecuted, &u.ExecExecuted, &u.APICalls, &u.StorageBytes, &u.BucketBytes,
		&u.CallsSuccess, &u.CallsClientErr, &u.CallsServerErr)
	if err != nil {
		return nil, fmt.Errorf("control: get usage: %w", err)
	}
	return u, nil
}

// ── Private helpers ───────────────────────────────────────────────────────────

func (c *ControlDB) getAPIKey(id string) (*APIKey, error) {
	return scanOneAPIKey(c.db.QueryRow(
		`SELECT id, user_id, name, scope, status, last_used_at, created_at
		 FROM api_keys WHERE id = ?`, id))
}

func scanDatabases(rows *sql.Rows) ([]Database, error) {
	var out []Database
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.DisplayName,
			&d.Description, &d.InstanceID, &d.Status, &d.SizeBytes, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanOneDatabase(row *sql.Row) (*Database, error) {
	d := &Database{}
	err := row.Scan(&d.ID, &d.UserID, &d.Name, &d.DisplayName,
		&d.Description, &d.InstanceID, &d.Status, &d.SizeBytes, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func scanBuckets(rows *sql.Rows) ([]Bucket, error) {
	var out []Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.ID, &b.UserID, &b.Name, &b.DisplayName,
			&b.Description, &b.InstanceID, &b.Status, &b.SizeBytes, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func scanOneBucket(row *sql.Row) (*Bucket, error) {
	b := &Bucket{}
	err := row.Scan(&b.ID, &b.UserID, &b.Name, &b.DisplayName,
		&b.Description, &b.InstanceID, &b.Status, &b.SizeBytes, &b.CreatedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

func scanAPIKeys(rows *sql.Rows) ([]APIKey, error) {
	var out []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.Scope,
			&k.Status, &k.LastUsedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func scanOneAPIKey(row *sql.Row) (*APIKey, error) {
	k := &APIKey{}
	err := row.Scan(&k.ID, &k.UserID, &k.Name, &k.Scope,
		&k.Status, &k.LastUsedAt, &k.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return k, err
}

// newID returns a random hex ID (16 bytes = 32 hex chars).
func newID() string { return randomHex(16) }

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based (should never happen)
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return strings.ToLower(hex.EncodeToString(b))
}
