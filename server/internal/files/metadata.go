// Package files — metadata.go manages files/metadata.db, which stores file
// records and blob reference counts. Mirrors the metadata half of file-storage.ts.
package files

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// StoredFile mirrors a row from the `files` table.
type StoredFile struct {
	ID          string
	DBName      string
	ContentHash string
	Filename    string
	FolderPath  string
	ContentType sql.NullString
	SizeBytes   int64
	StoragePath string
	UploadedAt  string
	ExpiresAt   sql.NullString
	Metadata    sql.NullString
}

// ListFilesResult is the paginated list response.
type ListFilesResult struct {
	Files  []StoredFile
	Total  int64
	Offset int
	Limit  int
}

// MetricsResult holds storage usage aggregates.
type MetricsResult struct {
	TotalFiles int64
	TotalBytes int64
	ByDatabase map[string]DBUsage
}

// DBUsage is the per-database usage entry in MetricsResult.
type DBUsage struct {
	Files int64
	Bytes int64
}

const metadataSchema = `
CREATE TABLE IF NOT EXISTS files (
  id           TEXT PRIMARY KEY,
  db_name      TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  filename     TEXT NOT NULL,
  folder_path  TEXT NOT NULL DEFAULT '',
  content_type TEXT,
  size_bytes   INTEGER NOT NULL,
  storage_path TEXT NOT NULL,
  uploaded_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  expires_at   DATETIME,
  metadata     TEXT
);
CREATE INDEX IF NOT EXISTS idx_files_db_name    ON files(db_name);
CREATE INDEX IF NOT EXISTS idx_files_hash       ON files(content_hash);
CREATE INDEX IF NOT EXISTS idx_files_expires    ON files(expires_at);
CREATE INDEX IF NOT EXISTS idx_files_db_folder  ON files(db_name, folder_path);

CREATE TABLE IF NOT EXISTS blob_refs (
  content_hash TEXT PRIMARY KEY,
  ref_count    INTEGER NOT NULL DEFAULT 1,
  first_seen   DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// MetadataDB wraps the files/metadata.db connection.
type MetadataDB struct {
	db *sql.DB
}

// OpenMetadataDB opens (or creates) the metadata database at filesRoot.
func OpenMetadataDB(filesRoot string) (*MetadataDB, error) {
	if err := os.MkdirAll(filepath.Join(filesRoot, "blobs"), 0o755); err != nil {
		return nil, fmt.Errorf("files: mkdir blobs: %w", err)
	}
	dbPath := filepath.Join(filesRoot, "metadata.db")

	// Use the same open() helper from the db package via a local inline here —
	// we import sqlite3 directly since files is a separate package.
	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=10000")
	if err != nil {
		return nil, fmt.Errorf("files: open metadata.db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	_, err = db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA synchronous=NORMAL;
		PRAGMA busy_timeout=10000;
		PRAGMA foreign_keys=ON;
	`)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("files: pragma setup: %w", err)
	}
	if _, err := db.Exec(metadataSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("files: schema: %w", err)
	}
	if err := migrateMetadata(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("files: migrate: %w", err)
	}
	return &MetadataDB{db: db}, nil
}

func migrateMetadata(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(files)")
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
	if !existing["folder_path"] {
		if _, err := db.Exec("ALTER TABLE files ADD COLUMN folder_path TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_files_db_folder ON files(db_name, folder_path)")
	}
	return nil
}

// Close shuts down the metadata connection.
func (m *MetadataDB) Close() error { return m.db.Close() }

// ── File record queries ───────────────────────────────────────────────────────

// GetByLogicalPath finds a file by (dbName, folderPath, filename).
// Returns nil, nil if not found.
func (m *MetadataDB) GetByLogicalPath(dbName, folderPath, filename string) (*StoredFile, error) {
	row := m.db.QueryRow(
		`SELECT id, db_name, content_hash, filename, folder_path, content_type,
		        size_bytes, storage_path, uploaded_at, expires_at, metadata
		 FROM files
		 WHERE db_name = ? AND folder_path = ? AND filename = ?
		 ORDER BY uploaded_at DESC, id DESC LIMIT 1`,
		dbName, folderPath, filename)
	f, err := scanFile(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

// GetByID returns a file by its UUID.
func (m *MetadataDB) GetByID(id string) (*StoredFile, error) {
	row := m.db.QueryRow(
		`SELECT id, db_name, content_hash, filename, folder_path, content_type,
		        size_bytes, storage_path, uploaded_at, expires_at, metadata
		 FROM files WHERE id = ?`, id)
	f, err := scanFile(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

// validSortColumns is the whitelist of columns that may be used for ordering.
var validSortColumns = map[string]string{
	"uploaded_at":  "uploaded_at",
	"filename":     "filename",
	"size_bytes":   "size_bytes",
	"content_type": "content_type",
}

// List returns paginated files for a database with optional folder prefix and
// sort / order params. sort must be one of the validSortColumns keys; order
// must be "asc" or "desc". Defaults: uploaded_at DESC.
func (m *MetadataDB) List(dbName string, limit, offset int, folderPrefix, sort, order string) (ListFilesResult, error) {
	var res ListFilesResult
	res.Limit = limit
	res.Offset = offset

	// Validate and resolve sort column against whitelist (prevents SQL injection).
	sortCol, ok := validSortColumns[sort]
	if !ok {
		sortCol = "uploaded_at"
	}
	if order != "asc" && order != "desc" {
		order = "desc"
	}

	countArgs := []any{dbName}
	countSQL := "SELECT COUNT(*) FROM files WHERE db_name = ?"
	listSQL := `SELECT id, db_name, content_hash, filename, folder_path, content_type,
	                    size_bytes, storage_path, uploaded_at, expires_at, metadata
	             FROM files WHERE db_name = ?`
	listArgs := []any{dbName}

	if folderPrefix != "" {
		countSQL += " AND folder_path LIKE ?"
		listSQL += " AND folder_path LIKE ?"
		countArgs = append(countArgs, folderPrefix+"%")
		listArgs = append(listArgs, folderPrefix+"%")
	}

	if err := m.db.QueryRow(countSQL, countArgs...).Scan(&res.Total); err != nil {
		return res, err
	}

	// #nosec G201 — sortCol and order are validated against whitelists above.
	listSQL += fmt.Sprintf(" ORDER BY %s %s LIMIT ? OFFSET ?", sortCol, order)
	listArgs = append(listArgs, limit, offset)

	rows, err := m.db.Query(listSQL, listArgs...)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	for rows.Next() {
		f, err := scanFileRow(rows)
		if err != nil {
			return res, err
		}
		res.Files = append(res.Files, *f)
	}
	return res, rows.Err()
}

// DBUsageStats returns the file count and total bytes for a database.
func (m *MetadataDB) DBUsageStats(dbName string) (fileCount, totalBytes int64, err error) {
	err = m.db.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM files WHERE db_name = ?",
		dbName).Scan(&fileCount, &totalBytes)
	return
}

// ── Writes ────────────────────────────────────────────────────────────────────

// InsertFile records a newly stored file.
func (m *MetadataDB) InsertFile(f *StoredFile) error {
	_, err := m.db.Exec(
		`INSERT INTO files
		   (id, db_name, content_hash, filename, folder_path, content_type,
		    size_bytes, storage_path, uploaded_at, expires_at, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.DBName, f.ContentHash, f.Filename, f.FolderPath, f.ContentType,
		f.SizeBytes, f.StoragePath, f.UploadedAt, f.ExpiresAt, f.Metadata)
	return err
}

// ReplaceFile replaces an existing file record by ID (used for conflict_mode=replace).
func (m *MetadataDB) ReplaceFile(oldID string, f *StoredFile) (oldHash string, err error) {
	tx, err := m.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err = tx.QueryRow("SELECT content_hash FROM files WHERE id = ?", oldID).Scan(&oldHash); err != nil {
		return
	}
	_, err = tx.Exec("DELETE FROM files WHERE id = ?", oldID)
	if err != nil {
		return
	}
	_, err = tx.Exec(
		`INSERT INTO files
		   (id, db_name, content_hash, filename, folder_path, content_type,
		    size_bytes, storage_path, uploaded_at, expires_at, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.DBName, f.ContentHash, f.Filename, f.FolderPath, f.ContentType,
		f.SizeBytes, f.StoragePath, f.UploadedAt, f.ExpiresAt, f.Metadata)
	if err != nil {
		return
	}
	err = tx.Commit()
	return
}

// DeleteFile removes a file record and returns its content_hash for blob GC.
func (m *MetadataDB) DeleteFile(id string) (contentHash string, err error) {
	err = m.db.QueryRow("SELECT content_hash FROM files WHERE id = ?", id).Scan(&contentHash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	_, err = m.db.Exec("DELETE FROM files WHERE id = ?", id)
	return
}

// DeleteFilesForDatabase removes all file records for a database and returns
// the set of content hashes so the caller can GC blobs.
func (m *MetadataDB) DeleteFilesForDatabase(dbName string) (hashes []string, err error) {
	rows, err := m.db.Query("SELECT DISTINCT content_hash FROM files WHERE db_name = ?", dbName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		hashes = append(hashes, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_, err = m.db.Exec("DELETE FROM files WHERE db_name = ?", dbName)
	return
}

// CleanupExpiredFiles deletes files whose expires_at has passed.
func (m *MetadataDB) CleanupExpiredFiles() (hashes []string, err error) {
	rows, err := m.db.Query(
		"SELECT DISTINCT content_hash FROM files WHERE expires_at IS NOT NULL AND datetime(expires_at) <= datetime('now')")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		hashes = append(hashes, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_, err = m.db.Exec(
		"DELETE FROM files WHERE expires_at IS NOT NULL AND datetime(expires_at) <= datetime('now')")
	return
}

// ── Blob ref counting ─────────────────────────────────────────────────────────

// IncrBlobRef increments (or creates) the ref count for a content hash.
func (m *MetadataDB) IncrBlobRef(hash string) error {
	_, err := m.db.Exec(
		`INSERT INTO blob_refs (content_hash, ref_count) VALUES (?, 1)
		 ON CONFLICT(content_hash) DO UPDATE SET ref_count = ref_count + 1`,
		hash)
	return err
}

// DecrBlobRef decrements the ref count and returns the new count.
// If the count reaches 0 the row is deleted and the caller should remove the blob file.
func (m *MetadataDB) DecrBlobRef(hash string) (remaining int64, err error) {
	tx, err := m.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.Exec(
		"UPDATE blob_refs SET ref_count = ref_count - 1 WHERE content_hash = ?", hash)
	if err != nil {
		return 0, err
	}
	err = tx.QueryRow("SELECT ref_count FROM blob_refs WHERE content_hash = ?", hash).Scan(&remaining)
	if err == sql.ErrNoRows {
		remaining = 0
		err = nil
	}
	if err != nil {
		return 0, err
	}
	if remaining <= 0 {
		_, err = tx.Exec("DELETE FROM blob_refs WHERE content_hash = ?", hash)
		if err != nil {
			return 0, err
		}
	}
	err = tx.Commit()
	return
}

// StorageMetrics returns aggregate usage across all databases.
func (m *MetadataDB) StorageMetrics() (MetricsResult, error) {
	var res MetricsResult
	if err := m.db.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM files").
		Scan(&res.TotalFiles, &res.TotalBytes); err != nil {
		return res, err
	}
	rows, err := m.db.Query(
		"SELECT db_name, COUNT(*), COALESCE(SUM(size_bytes), 0) FROM files GROUP BY db_name")
	if err != nil {
		return res, err
	}
	defer rows.Close()
	res.ByDatabase = map[string]DBUsage{}
	for rows.Next() {
		var dbn string
		var u DBUsage
		if err := rows.Scan(&dbn, &u.Files, &u.Bytes); err != nil {
			return res, err
		}
		res.ByDatabase[dbn] = u
	}
	return res, rows.Err()
}

// ── scan helpers ──────────────────────────────────────────────────────────────

func scanFile(row *sql.Row) (*StoredFile, error) {
	var f StoredFile
	err := row.Scan(&f.ID, &f.DBName, &f.ContentHash, &f.Filename, &f.FolderPath,
		&f.ContentType, &f.SizeBytes, &f.StoragePath, &f.UploadedAt, &f.ExpiresAt, &f.Metadata)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func scanFileRow(rows *sql.Rows) (*StoredFile, error) {
	var f StoredFile
	err := rows.Scan(&f.ID, &f.DBName, &f.ContentHash, &f.Filename, &f.FolderPath,
		&f.ContentType, &f.SizeBytes, &f.StoragePath, &f.UploadedAt, &f.ExpiresAt, &f.Metadata)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// NowUTC returns the current time formatted as SQLite datetime string.
func NowUTC() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}
