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
	Owner        string
	Description  sql.NullString
	CreatedAt    string
	Status       string
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
CREATE TABLE IF NOT EXISTS databases (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  name           TEXT UNIQUE NOT NULL,
  owner          TEXT NOT NULL,
  description    TEXT,
  created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
  status         TEXT NOT NULL DEFAULT 'active',
  original_name  TEXT,
  deleted_at     DATETIME
);

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
`

// OpenRegistry opens (or creates) registry.db at dataPath and applies the
// schema. It is idempotent — safe to call on every startup.
func OpenRegistry(dataPath string) (*Registry, error) {
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		return nil, fmt.Errorf("registry: mkdir %s: %w", dataPath, err)
	}
	dbPath := filepath.Join(dataPath, "registry.db")
	db, err := open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("registry: open: %w", err)
	}
	if _, err := db.Exec(registrySchema); err != nil {
		return nil, fmt.Errorf("registry: schema: %w", err)
	}
	if err := migrateRegistry(db); err != nil {
		return nil, fmt.Errorf("registry: migrate: %w", err)
	}
	return &Registry{db: db, dataPath: dataPath}, nil
}

// migrateRegistry adds columns introduced after the initial schema without
// dropping data — mirrors the column checks in registry.ts.
func migrateRegistry(db *sql.DB) error {
	type col struct{ name string }
	rows, err := db.Query("PRAGMA table_info(databases)")
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	migrations := []struct {
		col string
		ddl string
	}{
		{"original_name", "ALTER TABLE databases ADD COLUMN original_name TEXT"},
		{"deleted_at", "ALTER TABLE databases ADD COLUMN deleted_at DATETIME"},
	}
	for _, m := range migrations {
		if !existing[m.col] {
			if _, err := db.Exec(m.ddl); err != nil {
				return fmt.Errorf("add column %s: %w", m.col, err)
			}
		}
	}
	return nil
}

// Close shuts down the registry connection.
func (r *Registry) Close() error { return r.db.Close() }

// ── CRUD ─────────────────────────────────────────────────────────────────────

// ListDatabases returns all non-deleted databases ordered by created_at desc.
func (r *Registry) ListDatabases() ([]DBRecord, error) {
	rows, err := r.db.Query(
		`SELECT id, name, owner, description, created_at, status, original_name, deleted_at
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
		`SELECT id, name, owner, description, created_at, status, original_name, deleted_at
		 FROM databases WHERE name = ?`, name)
	rec, err := scanDBRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rec, err
}

// InsertDatabase creates a new database record.
func (r *Registry) InsertDatabase(name, owner string, description *string) (*DBRecord, error) {
	_, err := r.db.Exec(
		`INSERT INTO databases (name, owner, description) VALUES (?, ?, ?)`,
		name, owner, strPtr(description))
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

// ListDeletedDatabases returns all soft-deleted databases.
func (r *Registry) ListDeletedDatabases() ([]DBRecord, error) {
	rows, err := r.db.Query(
		`SELECT id, name, owner, description, created_at, status, original_name, deleted_at
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
		&r.ID, &r.Name, &r.Owner, &r.Description,
		&r.CreatedAt, &r.Status, &r.OriginalName, &r.DeletedAt)
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
			&r.ID, &r.Name, &r.Owner, &r.Description,
			&r.CreatedAt, &r.Status, &r.OriginalName, &r.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
