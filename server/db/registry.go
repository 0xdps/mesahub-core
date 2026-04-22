// Package db — registry.go manages registry.db, the single source of truth
// for all user databases on this instance. It mirrors registry.ts exactly.
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Registry wraps the shared registry.db connection.
type Registry struct {
	db       *sql.DB
	dataPath string
}

// DBRecord mirrors the `databases` table row.
type DBRecord struct {
	ID           int64
	Name         string
	Slug         sql.NullString
	DisplayName  string
	Owner        string
	Source       string
	InstanceID   sql.NullString
	Description  sql.NullString
	CreatedAt    string
	UpdatedAt    sql.NullString
	Status       string
	SizeBytes    int64
	OriginalName sql.NullString
	DeletedAt    sql.NullString
}

// AuditMetrics holds aggregated audit stats.
type AuditMetrics struct {
	TotalEvents   int64
	EventsLast24h int64
	ByTypeLast24h map[string]int64
}

const registrySchema = `
CREATE TABLE IF NOT EXISTS api_keys (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  key_hash     TEXT NOT NULL UNIQUE,
  scopes       TEXT NOT NULL DEFAULT '["all:w"]',
  owner        TEXT NOT NULL DEFAULT 'admin',
  key_type     TEXT NOT NULL DEFAULT 'admin',
  expires_at   TEXT,
  status       TEXT NOT NULL DEFAULT 'active',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS buckets (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  description  TEXT,
  owner        TEXT NOT NULL DEFAULT 'admin',
  source       TEXT NOT NULL DEFAULT 'template',
  slug         TEXT UNIQUE,
  instance_id  TEXT,
  status       TEXT NOT NULL DEFAULT 'active',
  size_bytes   INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_buckets_owner ON buckets(owner);

CREATE TABLE IF NOT EXISTS databases (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  name           TEXT NOT NULL,
  slug           TEXT UNIQUE,
  display_name   TEXT NOT NULL DEFAULT '',
  owner          TEXT NOT NULL DEFAULT 'admin',
  source         TEXT NOT NULL DEFAULT 'template',
  instance_id    TEXT,
  description    TEXT,
  status         TEXT NOT NULL DEFAULT 'active',
  size_bytes     INTEGER NOT NULL DEFAULT 0,
  original_name  TEXT,
  created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at     TEXT,
  deleted_at     DATETIME,
  UNIQUE(owner, name)
);
CREATE INDEX IF NOT EXISTS idx_databases_owner ON databases(owner);
CREATE UNIQUE INDEX IF NOT EXISTS idx_databases_slug ON databases(slug) WHERE slug IS NOT NULL;

CREATE TABLE IF NOT EXISTS file_token_revocations (
  token_id   TEXT PRIMARY KEY,
  db_name    TEXT NOT NULL,
  expires_at DATETIME NOT NULL,
  reason     TEXT,
  revoked_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_ftr_db_name   ON file_token_revocations(db_name);
CREATE INDEX IF NOT EXISTS idx_ftr_expires   ON file_token_revocations(expires_at);

CREATE TABLE IF NOT EXISTS audit_events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  event_type TEXT NOT NULL,
  db_name    TEXT,
  actor      TEXT,
  metadata   TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_audit_created   ON audit_events(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_db_name   ON audit_events(db_name);
CREATE INDEX IF NOT EXISTS idx_audit_event_type ON audit_events(event_type);

CREATE TABLE IF NOT EXISTS schema_migrations (
  name       TEXT PRIMARY KEY,
  applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// OpenRegistry opens (or creates) registry.db at dataPath and applies the
// schema. It is idempotent — safe to call on every startup.
func OpenRegistry(dataPath string) (*Registry, error) {
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		return nil, fmt.Errorf("registry: mkdir %s: %w", dataPath, err)
	}
	dbPath := filepath.Join(dataPath, "store.db")
	db, err := open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("registry: open: %w", err)
	}
	if _, err := db.Exec(registrySchema); err != nil {
		return nil, fmt.Errorf("registry: schema: %w", err)
	}
	return &Registry{db: db, dataPath: dataPath}, nil
}

// Close shuts down the registry connection.
func (r *Registry) Close() error { return r.db.Close() }

// RunOnce executes fn exactly once, identified by name. If name is already
// recorded in schema_migrations, fn is skipped entirely. On success, the name
// is inserted so subsequent calls are no-ops.
func (r *Registry) RunOnce(name string, fn func() error) error {
	var applied string
	err := r.db.QueryRow(`SELECT name FROM schema_migrations WHERE name = ?`, name).Scan(&applied)
	if err == nil {
		return nil // already ran
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("schema_migrations lookup: %w", err)
	}
	if err := fn(); err != nil {
		return err
	}
	_, err = r.db.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name)
	return err
}

// ── CRUD ─────────────────────────────────────────────────────────────────────

// ListDatabases returns all non-deleted databases ordered by created_at desc.
func (r *Registry) ListDatabases() ([]DBRecord, error) {
	rows, err := r.db.Query(
		`SELECT id, name, slug, display_name, owner, source, instance_id, description,
		        created_at, updated_at, status, size_bytes, original_name, deleted_at
		 FROM databases WHERE status != 'deleted' ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDBRecords(rows)
}

// GetDatabase returns the database record for name (any status).
func (r *Registry) GetDatabase(name string) (*DBRecord, error) {
	row := r.db.QueryRow(
		`SELECT id, name, slug, display_name, owner, source, instance_id, description,
		        created_at, updated_at, status, size_bytes, original_name, deleted_at
		 FROM databases WHERE name = ?`, name)
	rec, err := scanDBRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rec, err
}

// InsertDatabase creates a new database record.
// slug is the control-plane UUID (SaaS mode) or nil (template mode).
// displayName is the human-readable label; defaults to name when empty.
func (r *Registry) InsertDatabase(name, owner, source string, instanceID *string, description *string, slug *string, displayName string) (*DBRecord, error) {
	dn := displayName
	if dn == "" {
		dn = name
	}
	_, err := r.db.Exec(
		`INSERT INTO databases (name, slug, display_name, owner, source, instance_id, description) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, strPtr(slug), dn, owner, source, strPtr(instanceID), strPtr(description))
	if err != nil {
		return nil, err
	}
	return r.GetDatabase(name)
}

// SetDatabaseStatus updates the status field to 'active' or 'inactive'.
func (r *Registry) SetDatabaseStatus(name, status string) error {
	_, err := r.db.Exec(`UPDATE databases SET status = ? WHERE name = ?`, status, name)
	return err
}

// SoftDeleteDatabase renames the DB file and marks the record as deleted.
// Mirrors softDeleteDatabase() in registry.ts.
func (r *Registry) SoftDeleteDatabase(pool *Pool, name string) error {
	epoch := time.Now().Unix()
	newName := fmt.Sprintf("%s-%d", name, epoch)

	// Rename the DB file (and WAL/SHM sidecars) if they exist.
	oldBase := filepath.Join(r.dataPath, name+".db")
	newBase := filepath.Join(r.dataPath, newName+".db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		old := oldBase + suffix
		nw := newBase + suffix
		if _, statErr := os.Stat(old); statErr == nil {
			if err := os.Rename(old, nw); err != nil {
				return fmt.Errorf("rename %s: %w", old, err)
			}
		}
	}

	// Close pool connection so WAL is checkpointed before rename.
	pool.mu.Lock()
	if db, ok := pool.conns[name]; ok {
		_ = db.Close()
		delete(pool.conns, name)
	}
	pool.mu.Unlock()

	_, err := r.db.Exec(
		`UPDATE databases
		 SET name = ?, original_name = ?, status = 'deleted', deleted_at = datetime('now')
		 WHERE name = ?`,
		newName, name, name)
	return err
}

// GetDatabaseBySlug returns a database record by its slug.
func (r *Registry) GetDatabaseBySlug(slug string) (*DBRecord, error) {
	row := r.db.QueryRow(
		`SELECT id, name, slug, display_name, owner, source, instance_id, description,
		        created_at, updated_at, status, size_bytes, original_name, deleted_at
		 FROM databases WHERE slug = ?`, slug)
	rec, err := scanDBRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rec, err
}

// ListDatabasesByOwner returns all non-deleted databases for a given owner.
func (r *Registry) ListDatabasesByOwner(owner string) ([]DBRecord, error) {
	rows, err := r.db.Query(
		`SELECT id, name, slug, display_name, owner, source, instance_id, description,
		        created_at, updated_at, status, size_bytes, original_name, deleted_at
		 FROM databases WHERE owner = ? AND status != 'deleted' ORDER BY created_at DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDBRecords(rows)
}

// ListBucketsByOwner returns all active buckets for a given owner.
func (r *Registry) ListBucketsByOwner(owner string) ([]BucketRecord, error) {
	rows, err := r.db.Query(
		`SELECT id, name, display_name, description, owner, source, slug, instance_id,
		        status, size_bytes, created_at
		 FROM buckets WHERE owner = ? AND status = 'active' ORDER BY created_at DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBucketRecords(rows)
}

// ListAPIKeysByOwner returns all active API keys for a given owner.
func (r *Registry) ListAPIKeysByOwner(owner string) ([]APIKeyRecord, error) {
	rows, err := r.db.Query(
		`SELECT id, name, key_hash, scopes, owner, key_type,
		        expires_at, status, last_used_at, created_at
		 FROM api_keys WHERE owner = ? AND status = 'active' ORDER BY created_at DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAPIKeyRecords(rows)
}

// ListDeletedDatabases returns all soft-deleted databases.
func (r *Registry) ListDeletedDatabases() ([]DBRecord, error) {
	rows, err := r.db.Query(
		`SELECT id, name, slug, display_name, owner, source, instance_id, description,
		        created_at, updated_at, status, size_bytes, original_name, deleted_at
		 FROM databases WHERE status = 'deleted' ORDER BY deleted_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDBRecords(rows)
}

// RestoreDatabase un-deletes a soft-deleted database.
func (r *Registry) RestoreDatabase(pool *Pool, deletedName string) (*DBRecord, error) {
	rec, err := r.GetDatabase(deletedName)
	if err != nil {
		return nil, err
	}
	if rec == nil || rec.Status != "deleted" || !rec.OriginalName.Valid {
		return nil, fmt.Errorf("database %q not found or not in deleted state", deletedName)
	}

	originalName := rec.OriginalName.String
	oldBase := filepath.Join(r.dataPath, deletedName+".db")
	newBase := filepath.Join(r.dataPath, originalName+".db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		old := oldBase + suffix
		nw := newBase + suffix
		if _, statErr := os.Stat(old); statErr == nil {
			if err := os.Rename(old, nw); err != nil {
				return nil, fmt.Errorf("rename %s: %w", old, err)
			}
		}
	}

	_, err = r.db.Exec(
		`UPDATE databases SET name = ?, original_name = NULL, status = 'active', deleted_at = NULL WHERE name = ?`,
		originalName, deletedName)
	if err != nil {
		return nil, err
	}
	return r.GetDatabase(originalName)
}

// HardDeleteDatabase permanently removes a soft-deleted database.
func (r *Registry) HardDeleteDatabase(deletedName string) error {
	rec, err := r.GetDatabase(deletedName)
	if err != nil {
		return err
	}
	if rec == nil || rec.Status != "deleted" {
		return fmt.Errorf("database %q not found or not in deleted state", deletedName)
	}

	dbPath := filepath.Join(r.dataPath, deletedName+".db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		f := dbPath + suffix
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", f, err)
		}
	}

	_, err = r.db.Exec(`DELETE FROM databases WHERE name = ?`, deletedName)
	return err
}

// ── File token revocations ────────────────────────────────────────────────────

// RevokeFileToken inserts or upserts a revocation record for a file access token.
func (r *Registry) RevokeFileToken(tokenID, dbName, expiresAt string, reason *string) error {
	_, err := r.db.Exec(
		`INSERT INTO file_token_revocations (token_id, db_name, expires_at, reason)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(token_id) DO UPDATE SET
		   db_name = excluded.db_name,
		   expires_at = excluded.expires_at,
		   reason = excluded.reason,
		   revoked_at = CURRENT_TIMESTAMP`,
		tokenID, dbName, expiresAt, strPtr(reason))
	return err
}

// IsFileTokenRevoked returns true if the token has an active revocation.
func (r *Registry) IsFileTokenRevoked(tokenID, dbName string) (bool, error) {
	var id string
	err := r.db.QueryRow(
		`SELECT token_id FROM file_token_revocations
		 WHERE token_id = ? AND db_name = ? AND datetime(expires_at) > datetime('now')
		 LIMIT 1`,
		tokenID, dbName).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CleanupExpiredTokenRevocations deletes expired revocation records.
func (r *Registry) CleanupExpiredTokenRevocations() (int64, error) {
	res, err := r.db.Exec(
		`DELETE FROM file_token_revocations WHERE datetime(expires_at) <= datetime('now')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ── Audit events ──────────────────────────────────────────────────────────────

// RecordAuditEvent appends an audit log entry.
func (r *Registry) RecordAuditEvent(eventType string, dbName, actor *string, metadata map[string]any) error {
	var metaStr *string
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		s := string(b)
		metaStr = &s
	}
	_, err := r.db.Exec(
		`INSERT INTO audit_events (event_type, db_name, actor, metadata) VALUES (?, ?, ?, ?)`,
		eventType, strPtr(dbName), strPtr(actor), metaStr)
	return err
}

// GetAuditMetrics returns aggregate audit statistics.
func (r *Registry) GetAuditMetrics() (AuditMetrics, error) {
	var m AuditMetrics
	err := r.db.QueryRow(
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN datetime(created_at) >= datetime('now', '-1 day') THEN 1 ELSE 0 END), 0)
		 FROM audit_events`).Scan(&m.TotalEvents, &m.EventsLast24h)
	if err != nil {
		return m, err
	}

	rows, err := r.db.Query(
		`SELECT event_type, COUNT(*) FROM audit_events
		 WHERE datetime(created_at) >= datetime('now', '-1 day')
		 GROUP BY event_type`)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	m.ByTypeLast24h = map[string]int64{}
	for rows.Next() {
		var typ string
		var cnt int64
		if err := rows.Scan(&typ, &cnt); err != nil {
			return m, err
		}
		m.ByTypeLast24h[typ] = cnt
	}
	return m, rows.Err()
}

// ── API keys ──────────────────────────────────────────────────────────────────

// APIKeyRecord is a row from the api_keys table.
type APIKeyRecord struct {
	ID         string
	Name       string
	KeyHash    string
	Scopes     string
	Owner      string
	KeyType    string
	ExpiresAt  sql.NullString
	Status     string
	LastUsedAt sql.NullString
	CreatedAt  string
}

// InsertAPIKey inserts a new API key.
func (r *Registry) InsertAPIKey(id, name, keyHash, scopes, owner, keyType string, expiresAt *string) (*APIKeyRecord, error) {
	_, err := r.db.Exec(
		`INSERT INTO api_keys (id, name, key_hash, scopes, owner, key_type, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, name, keyHash, scopes, owner, keyType, strPtr(expiresAt))
	if err != nil {
		return nil, err
	}
	return r.GetAPIKeyByID(id)
}

// GetAPIKeyByID returns a key record by its ID.
func (r *Registry) GetAPIKeyByID(id string) (*APIKeyRecord, error) {
	row := r.db.QueryRow(
		`SELECT id, name, key_hash, scopes, owner, key_type,
		        expires_at, status, last_used_at, created_at
		 FROM api_keys WHERE id = ?`, id)
	return scanAPIKeyRecord(row)
}

// GetAPIKeyByHash returns a key record by its SHA-256 hash (used during auth).
func (r *Registry) GetAPIKeyByHash(hash string) (*APIKeyRecord, error) {
	row := r.db.QueryRow(
		`SELECT id, name, key_hash, scopes, owner, key_type,
		        expires_at, status, last_used_at, created_at
		 FROM api_keys WHERE key_hash = ? AND status = 'active'`, hash)
	rec, err := scanAPIKeyRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rec, err
}

// ListAPIKeys returns all active API key records (hash only — raw value is never stored).
func (r *Registry) ListAPIKeys() ([]APIKeyRecord, error) {
	rows, err := r.db.Query(
		`SELECT id, name, key_hash, scopes, owner, key_type,
		        expires_at, status, last_used_at, created_at
		 FROM api_keys WHERE status = 'active' ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAPIKeyRecords(rows)
}

// RevokeAPIKey marks a key as revoked.
func (r *Registry) RevokeAPIKey(id string) error {
	_, err := r.db.Exec(`UPDATE api_keys SET status = 'revoked' WHERE id = ?`, id)
	return err
}

// TouchAPIKey updates last_used_at for a key.
func (r *Registry) TouchAPIKey(id string) error {
	_, err := r.db.Exec(
		`UPDATE api_keys SET last_used_at = datetime('now') WHERE id = ?`, id)
	return err
}

// ── Buckets ───────────────────────────────────────────────────────────────────

// BucketRecord is a row from the buckets table.
type BucketRecord struct {
	ID          string
	Name        string
	DisplayName string
	Description sql.NullString
	Owner       string
	Source      string
	Slug        sql.NullString
	InstanceID  sql.NullString
	Status      string
	SizeBytes   int64
	CreatedAt   string
}

// InsertBucket creates a new bucket record.
func (r *Registry) InsertBucket(id, name, displayName, owner, source string, instanceID *string, description *string) (*BucketRecord, error) {
	_, err := r.db.Exec(
		`INSERT INTO buckets (id, name, display_name, owner, source, instance_id, description) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, name, displayName, owner, source, strPtr(instanceID), strPtr(description))
	if err != nil {
		return nil, err
	}
	return r.GetBucket(name)
}

// GetBucketByID returns a bucket record by its UUID primary key.
func (r *Registry) GetBucketByID(id string) (*BucketRecord, error) {
	row := r.db.QueryRow(
		`SELECT id, name, display_name, description, owner, source, slug, instance_id,
		        status, size_bytes, created_at
		 FROM buckets WHERE id = ?`, id)
	rec, err := scanBucketRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rec, err
}

// GetBucketBySlug returns a bucket record by its slug (control-plane UUID).
func (r *Registry) GetBucketBySlug(slug string) (*BucketRecord, error) {
	row := r.db.QueryRow(
		`SELECT id, name, display_name, description, owner, source, slug, instance_id,
		        status, size_bytes, created_at
		 FROM buckets WHERE slug = ?`, slug)
	rec, err := scanBucketRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rec, err
}

// GetBucket returns a bucket record by name.
func (r *Registry) GetBucket(name string) (*BucketRecord, error) {
	row := r.db.QueryRow(
		`SELECT id, name, display_name, description, owner, source, slug, instance_id,
		        status, size_bytes, created_at
		 FROM buckets WHERE name = ?`, name)
	rec, err := scanBucketRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rec, err
}

// ListBuckets returns all active buckets.
func (r *Registry) ListBuckets() ([]BucketRecord, error) {
	rows, err := r.db.Query(
		`SELECT id, name, display_name, description, owner, source, slug, instance_id,
		        status, size_bytes, created_at
		 FROM buckets WHERE status = 'active' ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBucketRecords(rows)
}

// DeleteBucket marks a bucket as deleted.
func (r *Registry) DeleteBucket(name string) error {
	_, err := r.db.Exec(`UPDATE buckets SET status = 'deleted' WHERE name = ?`, name)
	return err
}

// UpdateBucketSize increments the stored size_bytes for a bucket.
func (r *Registry) UpdateBucketSize(name string, delta int64) error {
	_, err := r.db.Exec(
		`UPDATE buckets SET size_bytes = MAX(0, size_bytes + ?) WHERE name = ?`, delta, name)
	return err
}

// ── helpers ───────────────────────────────────────────────────────────────────

func strPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func scanDBRecord(row *sql.Row) (*DBRecord, error) {
	var r DBRecord
	err := row.Scan(
		&r.ID, &r.Name, &r.Slug, &r.DisplayName, &r.Owner, &r.Source, &r.InstanceID, &r.Description,
		&r.CreatedAt, &r.UpdatedAt, &r.Status, &r.SizeBytes, &r.OriginalName, &r.DeletedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func scanDBRecords(rows *sql.Rows) ([]DBRecord, error) {
	var out []DBRecord
	for rows.Next() {
		var r DBRecord
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Slug, &r.DisplayName, &r.Owner, &r.Source, &r.InstanceID, &r.Description,
			&r.CreatedAt, &r.UpdatedAt, &r.Status, &r.SizeBytes, &r.OriginalName, &r.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanAPIKeyRecord(row *sql.Row) (*APIKeyRecord, error) {
	var r APIKeyRecord
	err := row.Scan(&r.ID, &r.Name, &r.KeyHash, &r.Scopes,
		&r.Owner, &r.KeyType,
		&r.ExpiresAt, &r.Status, &r.LastUsedAt, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func scanAPIKeyRecords(rows *sql.Rows) ([]APIKeyRecord, error) {
	var out []APIKeyRecord
	for rows.Next() {
		var r APIKeyRecord
		if err := rows.Scan(&r.ID, &r.Name, &r.KeyHash, &r.Scopes,
			&r.Owner, &r.KeyType,
			&r.ExpiresAt, &r.Status, &r.LastUsedAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanBucketRecord(row *sql.Row) (*BucketRecord, error) {
	var r BucketRecord
	err := row.Scan(&r.ID, &r.Name, &r.DisplayName, &r.Description,
		&r.Owner, &r.Source, &r.Slug, &r.InstanceID,
		&r.Status, &r.SizeBytes, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func scanBucketRecords(rows *sql.Rows) ([]BucketRecord, error) {
	var out []BucketRecord
	for rows.Next() {
		var r BucketRecord
		if err := rows.Scan(&r.ID, &r.Name, &r.DisplayName, &r.Description,
			&r.Owner, &r.Source, &r.Slug, &r.InstanceID,
			&r.Status, &r.SizeBytes, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
