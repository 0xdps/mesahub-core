// Package control manages the control.db SQLite database used in
// SQLITE_HUB_MODE=control deployments.  It owns the schema, provides typed
// query helpers, and exposes a ControlDB type that wraps *sql.DB.
package control

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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
	name                TEXT,
  plan                TEXT NOT NULL DEFAULT 'free',
  status              TEXT NOT NULL DEFAULT 'active',
  license_status      TEXT DEFAULT '',
  entitlements        TEXT DEFAULT '{}',
  license_synced_at   TEXT,
  created_at          TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at          TEXT
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
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,          -- user-visible label
  slug         TEXT NOT NULL DEFAULT '',-- template-internal name (e.g. D-<userpart>-<dbname>)
  display_name TEXT NOT NULL,
  description  TEXT,
  instance_id  TEXT REFERENCES instances(id),
  status       TEXT NOT NULL DEFAULT 'active',
  size_bytes   INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at   TEXT,
  UNIQUE(user_id, name)
);

CREATE TABLE IF NOT EXISTS buckets (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,          -- user-visible label
  slug         TEXT NOT NULL DEFAULT '',-- template-internal name (e.g. B-<userpart>-<bucketname>)
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
-- scopes is a JSON array of permission strings. Suffix :r = read-only,
-- :w = read+write (write implies read).
-- Examples:
--   '["all:w"]'                  -> full access to all databases and buckets
--   '["all:r"]'                  -> read-only access to everything
--   '["db:*:w"]'                 -> full access to all databases
--   '["db:*:r"]'                 -> read-only to all databases
--   '["db:D-abc-mydb:w"]'        -> full access to a specific database
--   '["db:D-abc-mydb:r"]'        -> read-only to a specific database
--   '["bucket:B-abc-photos:r"]'  -> read-only to a specific bucket
CREATE TABLE IF NOT EXISTS api_keys (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  key_hash     TEXT NOT NULL UNIQUE,
  scopes       TEXT NOT NULL DEFAULT '["all:w"]',
  expires_at   TEXT,
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
CREATE UNIQUE INDEX IF NOT EXISTS idx_databases_slug ON databases(slug) WHERE slug != '';
CREATE INDEX IF NOT EXISTS idx_buckets_user_id   ON buckets(user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_buckets_slug ON buckets(slug) WHERE slug != '';
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id  ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_hash     ON api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_usage_user_period ON usage(user_id, period_year, period_month);

-- ── Billing tables ────────────────────────────────────────────────────────────
-- These tables are declared here so that control.db is fully initialised on
-- first process start.  They are queried exclusively by the control app over
-- HTTP; the Go server only creates them.

-- NubeAuth plan catalog.  Populated by syncCatalog() and kept current via
-- plan.* webhook events.  slug matches the value stored in users.plan.
CREATE TABLE IF NOT EXISTS plans (
  plan_id       TEXT PRIMARY KEY,
  slug          TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL,
  description   TEXT,
  display_order INTEGER NOT NULL DEFAULT 0,
  features      TEXT    NOT NULL DEFAULT '[]',
  is_deleted    INTEGER NOT NULL DEFAULT 0,
  synced_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- One or more prices per plan (monthly, yearly, one-time).
-- price_id is passed to payment.createCheckout().
CREATE TABLE IF NOT EXISTS prices (
  price_id      TEXT PRIMARY KEY,
  plan_id       TEXT NOT NULL REFERENCES plans(plan_id) ON DELETE CASCADE,
  billing_type  TEXT NOT NULL,
  interval      TEXT,
  amount_cents  INTEGER NOT NULL,
  currency      TEXT    NOT NULL DEFAULT 'usd',
  trial_enabled INTEGER NOT NULL DEFAULT 0,
  trial_days    INTEGER,
  is_active     INTEGER NOT NULL DEFAULT 1,
  synced_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_prices_plan_id ON prices (plan_id);

-- Audit trail for all received NubeAuth webhook deliveries.
-- delivery_id is the UUID from the X-Nube-Delivery header / event.id.
CREATE TABLE IF NOT EXISTS webhook_log (
  delivery_id  TEXT PRIMARY KEY,
  event        TEXT NOT NULL,
  app_id       TEXT,
  payload      TEXT NOT NULL,
  processed_at TEXT NOT NULL DEFAULT (datetime('now')),
  error        TEXT
);

CREATE INDEX IF NOT EXISTS idx_webhook_log_event ON webhook_log (event);

-- Latest subscription state per user, fed by webhooks and cancel/resume API
-- actions.  Billing UI reads from this for fast, stable status.
CREATE TABLE IF NOT EXISTS subscriptions (
  user_id              TEXT PRIMARY KEY,
  subscription_id      TEXT,
  plan_slug            TEXT,
  status               TEXT NOT NULL DEFAULT 'active',
  license_status       TEXT NOT NULL DEFAULT 'active',
  billing_interval     TEXT,
  period_end           TEXT,
  cancel_at_period_end INTEGER NOT NULL DEFAULT 0,
  last_event           TEXT,
  last_event_at        TEXT,
  last_cancel_reason   TEXT,
  last_cancel_comment  TEXT,
  updated_at           TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_status
  ON subscriptions (status, cancel_at_period_end);
`

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
	return &ControlDB{db: db}, nil
}

// Close releases the underlying database connection.
func (c *ControlDB) Close() error { return c.db.Close() }

// ── Types ─────────────────────────────────────────────────────────────────────

// User is a control-plane user record.
type User struct {
	ID              string  `json:"id"`
	Email           string  `json:"email"`
	Name            *string `json:"name,omitempty"`
	Plan            string  `json:"plan"`
	Status          string  `json:"status"`
	LicenseStatus   *string `json:"license_status,omitempty"`
	Entitlements    *string `json:"entitlements,omitempty"`
	LicenseSyncedAt *string `json:"license_synced_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       *string `json:"updated_at,omitempty"`
}

// Database is a control-plane database record.
type Database struct {
	ID          string  `json:"id"`
	UserID      string  `json:"user_id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
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
	Slug        string  `json:"slug"`
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
	ID         string   `json:"id"`
	UserID     string   `json:"user_id"`
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  *string  `json:"expires_at,omitempty"`
	Status     string   `json:"status"`
	LastUsedAt *string  `json:"last_used_at"`
	CreatedAt  string   `json:"created_at"`
}

// ── User ops ──────────────────────────────────────────────────────────────────

// UpsertUser inserts or updates the user identified by nubeID.
// Returns the stored user record.
func (c *ControlDB) UpsertUser(nubeID, email string, name *string, plan, status, licenseStatus, entitlements, licenseSyncedAt string) (*User, error) {
	_, err := c.db.Exec(`
		INSERT INTO users (id, email, name, plan, status, license_status, entitlements, license_synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  email             = excluded.email,
		  name              = excluded.name,
		  plan              = excluded.plan,
		  status            = excluded.status,
		  license_status    = excluded.license_status,
		  entitlements      = excluded.entitlements,
		  license_synced_at = excluded.license_synced_at`,
		nubeID, email, name, plan, status, licenseStatus, entitlements, licenseSyncedAt)
	if err != nil {
		return nil, fmt.Errorf("control: upsert user: %w", err)
	}
	return c.GetUser(nubeID)
}

// GetUser fetches a user by ID.
func (c *ControlDB) GetUser(id string) (*User, error) {
	u := &User{}
	err := c.db.QueryRow(
		`SELECT id, email, name, plan, status, license_status, entitlements, license_synced_at, created_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Plan, &u.Status, &u.LicenseStatus, &u.Entitlements, &u.LicenseSyncedAt, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// ── Database ops ──────────────────────────────────────────────────────────────

// ListDatabases returns all non-deleted databases for a user.
func (c *ControlDB) ListDatabases(userID string) ([]Database, error) {
	rows, err := c.db.Query(
		`SELECT id, user_id, name, slug, display_name, description, instance_id, status, size_bytes, created_at, updated_at
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
		`SELECT id, user_id, name, slug, display_name, description, instance_id, status, size_bytes, created_at, updated_at
		 FROM databases WHERE id = ?`, id))
}

// GetDatabaseByName fetches a database record by user-visible name.
func (c *ControlDB) GetDatabaseByName(name string) (*Database, error) {
	return scanOneDatabase(c.db.QueryRow(
		`SELECT id, user_id, name, slug, display_name, description, instance_id, status, size_bytes, created_at, updated_at
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
func (c *ControlDB) CreateDatabase(userID, name, slug, displayName string) (*Database, error) {
	id := newID()
	_, err := c.db.Exec(
		`INSERT INTO databases (id, user_id, name, slug, display_name) VALUES (?, ?, ?, ?, ?)`,
		id, userID, name, slug, displayName)
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
		`SELECT id, user_id, name, slug, display_name, description, instance_id, status, size_bytes, created_at, updated_at
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
		`SELECT id, user_id, name, slug, display_name, description, instance_id, status, size_bytes, created_at, updated_at
		 FROM buckets WHERE id = ?`, id))
}

// GetBucketByName fetches a bucket by user-visible name.
func (c *ControlDB) GetBucketByName(name string) (*Bucket, error) {
	return scanOneBucket(c.db.QueryRow(
		`SELECT id, user_id, name, slug, display_name, description, instance_id, status, size_bytes, created_at, updated_at
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
func (c *ControlDB) CreateBucket(userID, name, slug, displayName string, instanceID *string) (*Bucket, error) {
	id := newID()
	_, err := c.db.Exec(
		`INSERT INTO buckets (id, user_id, name, slug, display_name, instance_id) VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, name, slug, displayName, instanceID)
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
func (c *ControlDB) UpdateBucketSize(slug string, sizeBytes int64) error {
	_, err := c.db.Exec(
		`UPDATE buckets SET size_bytes = ? WHERE slug = ?`, sizeBytes, slug)
	return err
}

// ── API key ops ───────────────────────────────────────────────────────────────

// ListAPIKeys returns all active API keys for a user.
func (c *ControlDB) ListAPIKeys(userID string) ([]APIKey, error) {
	rows, err := c.db.Query(
		`SELECT id, user_id, name, scopes, expires_at, status, last_used_at, created_at
		 FROM api_keys WHERE user_id = ? AND status = 'active'
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAPIKeys(rows)
}

// CreateAPIKey generates a new API key and inserts it.
// scopes is a JSON array like ["all:w"], ["db:*:r"], ["db:D-abc-mydb:w"], etc.
// Returns the created APIKey and the raw key value (shown once only).
func (c *ControlDB) CreateAPIKey(userID, name string, scopes []string) (*APIKey, string, error) {
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return nil, "", fmt.Errorf("control: marshal scopes: %w", err)
	}

	rawKey := "shs_" + randomHex(32)
	sum := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(sum[:])

	id := newID()
	_, err = c.db.Exec(
		`INSERT INTO api_keys (id, user_id, name, key_hash, scopes) VALUES (?, ?, ?, ?, ?)`,
		id, userID, name, keyHash, string(scopesJSON))
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

// APIKeyAuthRow holds the minimal fields needed to authenticate an API key.
type APIKeyAuthRow struct {
	KeyID  string
	UserID string
	Scopes string // raw JSON
}

// LookupAPIKeyByHash resolves an active API key by its SHA-256 key_hash.
// It also verifies that the owning user is active.
// Returns nil, nil when no matching key is found.
func (c *ControlDB) LookupAPIKeyByHash(keyHash string) (*APIKeyAuthRow, error) {
	r := &APIKeyAuthRow{}
	err := c.db.QueryRow(`
		SELECT k.id, k.user_id, k.scopes
		FROM api_keys k
		JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = ? AND k.status = 'active' AND u.status = 'active'`,
		keyHash).Scan(&r.KeyID, &r.UserID, &r.Scopes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// StampAPIKeyLastUsed updates last_used_at for the given key ID.
// Best-effort: callers may safely ignore the returned error.
func (c *ControlDB) StampAPIKeyLastUsed(id string) error {
	_, err := c.db.Exec(`UPDATE api_keys SET last_used_at = datetime('now') WHERE id = ?`, id)
	return err
}

// LookupBucketOwner returns the user_id for an active bucket identified by slug.
// Returns "", nil when no matching bucket is found.
func (c *ControlDB) LookupBucketOwner(slug string) (string, error) {
	var userID string
	err := c.db.QueryRow(
		`SELECT user_id FROM buckets WHERE slug = ? AND status = 'active'`, slug,
	).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return userID, err
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
		`SELECT id, user_id, name, scopes, expires_at, status, last_used_at, created_at
		 FROM api_keys WHERE id = ?`, id))
}

func scanDatabases(rows *sql.Rows) ([]Database, error) {
	var out []Database
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.Slug, &d.DisplayName,
			&d.Description, &d.InstanceID, &d.Status, &d.SizeBytes, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanOneDatabase(row *sql.Row) (*Database, error) {
	d := &Database{}
	err := row.Scan(&d.ID, &d.UserID, &d.Name, &d.Slug, &d.DisplayName,
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
		if err := rows.Scan(&b.ID, &b.UserID, &b.Name, &b.Slug, &b.DisplayName,
			&b.Description, &b.InstanceID, &b.Status, &b.SizeBytes, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func scanOneBucket(row *sql.Row) (*Bucket, error) {
	b := &Bucket{}
	err := row.Scan(&b.ID, &b.UserID, &b.Name, &b.Slug, &b.DisplayName,
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
		var scopesJSON string
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &scopesJSON,
			&k.ExpiresAt, &k.Status, &k.LastUsedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(scopesJSON), &k.Scopes)
		out = append(out, k)
	}
	return out, rows.Err()
}

func scanOneAPIKey(row *sql.Row) (*APIKey, error) {
	k := &APIKey{}
	var scopesJSON string
	err := row.Scan(&k.ID, &k.UserID, &k.Name, &scopesJSON,
		&k.ExpiresAt, &k.Status, &k.LastUsedAt, &k.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(scopesJSON), &k.Scopes)
	return k, nil
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
