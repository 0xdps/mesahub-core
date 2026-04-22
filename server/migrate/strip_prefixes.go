// Package migrate contains one-time data migrations that run at server startup
// via Registry.RunOnce. Each migration is idempotent — it is a no-op if the
// data has already been migrated.
package migrate

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

// StripPrefixes removes the legacy "D-" prefix from database template-internal
// names and the "B-" prefix from bucket template-internal names across all
// storage layers:
//
//  1. On-disk .db files (and -wal / -shm sidecars) starting with "D-"
//  2. registry.db — databases.name, databases.original_name,
//     file_token_revocations.db_name, audit_events.db_name
//  3. files/metadata.db — files.db_name ("bkt-B-<x>" → "bkt-<x>")
func StripPrefixes(dataPath string) error {
	m := &prefixMigration{dataPath: dataPath}
	return m.run()
}

type prefixMigration struct {
	dataPath string
	renamed  int
	updated  int
}

func (m *prefixMigration) run() error {
	// Phase 1 first — rename files before touching DB rows so a crash between
	// phases leaves us in a re-runnable state.
	if err := m.renameDBFiles(); err != nil {
		return fmt.Errorf("phase 1 (rename files): %w", err)
	}
	if err := m.updateRegistry(); err != nil {
		return fmt.Errorf("phase 2 (registry.db): %w", err)
	}
	if err := m.updateFilesMetadata(); err != nil {
		return fmt.Errorf("phase 3 (files/metadata.db): %w", err)
	}
	log.Info().
		Int("files_renamed", m.renamed).
		Int("rows_updated", m.updated).
		Msg("migrate: strip-prefixes complete")
	return nil
}

// ── Phase 1: rename .db files ─────────────────────────────────────────────────

func (m *prefixMigration) renameDBFiles() error {
	regPath := filepath.Join(m.dataPath, "registry.db")
	if _, err := os.Stat(regPath); os.IsNotExist(err) {
		return nil
	}

	db, err := openDB(regPath)
	if err != nil {
		return err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT name FROM databases`)
	if err != nil {
		return fmt.Errorf("query registry names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, name := range names {
		if err := m.renameDBFile(name); err != nil {
			return err
		}
	}
	return nil
}

func (m *prefixMigration) renameDBFile(name string) error {
	newName, stripped := stripPrefix(name, "D-")
	if !stripped {
		return nil
	}
	oldBase := filepath.Join(m.dataPath, name+".db")
	newBase := filepath.Join(m.dataPath, newName+".db")
	for _, suf := range []string{"", "-wal", "-shm"} {
		old, nw := oldBase+suf, newBase+suf
		if _, err := os.Stat(old); os.IsNotExist(err) {
			continue
		}
		log.Info().Str("from", filepath.Base(old)).Str("to", filepath.Base(nw)).Msg("migrate: rename db file")
		if err := os.Rename(old, nw); err != nil {
			return fmt.Errorf("rename %s: %w", old, err)
		}
		m.renamed++
	}
	return nil
}

// ── Phase 2: registry.db ──────────────────────────────────────────────────────

func (m *prefixMigration) updateRegistry() error {
	regPath := filepath.Join(m.dataPath, "registry.db")
	if _, err := os.Stat(regPath); os.IsNotExist(err) {
		return nil
	}

	db, err := openDB(regPath)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	if err := m.updateColumnPrefix(tx, "databases", "name", "id", "D-"); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := m.updateColumnPrefix(tx, "databases", "original_name", "id", "D-"); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := m.updateTokenRevocationNames(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := m.updateAuditEventNames(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (m *prefixMigration) updateTokenRevocationNames(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT token_id, db_name FROM file_token_revocations`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct{ id, name string }
	var targets []row
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		targets = append(targets, row{id, name})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, t := range targets {
		newName := stripBucketOrDBPrefix(t.name)
		if newName == t.name {
			continue
		}
		if _, err := tx.Exec(`UPDATE file_token_revocations SET db_name = ? WHERE token_id = ?`, newName, t.id); err != nil {
			return fmt.Errorf("update token revocation %s: %w", t.id, err)
		}
		m.updated++
	}
	return nil
}

func (m *prefixMigration) updateAuditEventNames(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id, db_name FROM audit_events WHERE db_name IS NOT NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		id   int64
		name string
	}
	var targets []row
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		targets = append(targets, row{id, name})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, t := range targets {
		newName := stripBucketOrDBPrefix(t.name)
		if newName == t.name {
			continue
		}
		if _, err := tx.Exec(`UPDATE audit_events SET db_name = ? WHERE id = ?`, newName, t.id); err != nil {
			return fmt.Errorf("update audit event %d: %w", t.id, err)
		}
		m.updated++
	}
	return nil
}

// ── Phase 3: files/metadata.db ───────────────────────────────────────────────

func (m *prefixMigration) updateFilesMetadata() error {
	metaPath := filepath.Join(m.dataPath, "files", "metadata.db")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		return nil
	}

	db, err := openDB(metaPath)
	if err != nil {
		return err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT DISTINCT db_name FROM files WHERE db_name LIKE 'bkt-B-%'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var namespaces []string
	for rows.Next() {
		var ns string
		if err := rows.Scan(&ns); err != nil {
			return err
		}
		namespaces = append(namespaces, ns)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(namespaces) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, ns := range namespaces {
		newNS := "bkt-" + ns[len("bkt-B-"):]
		var count int
		_ = db.QueryRow(`SELECT COUNT(*) FROM files WHERE db_name = ?`, ns).Scan(&count)
		if _, err := tx.Exec(`UPDATE files SET db_name = ? WHERE db_name = ?`, newNS, ns); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update files.db_name %s: %w", ns, err)
		}
		m.updated += count
	}
	return tx.Commit()
}

// ── Shared helpers ────────────────────────────────────────────────────────────

// updateColumnPrefix strips prefix from all matching non-null values in
// table.col, keyed by pkCol, within the given transaction.
func (m *prefixMigration) updateColumnPrefix(tx *sql.Tx, table, col, pkCol, prefix string) error {
	query := fmt.Sprintf(`SELECT %s, %s FROM %s WHERE %s LIKE ?`, pkCol, col, table, col)
	rows, err := tx.Query(query, prefix+"%")
	if err != nil {
		return fmt.Errorf("%s.%s query: %w", table, col, err)
	}
	defer rows.Close()
	type row struct{ pk, val string }
	var targets []row
	for rows.Next() {
		var pk, val string
		if err := rows.Scan(&pk, &val); err != nil {
			return err
		}
		targets = append(targets, row{pk, val})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, t := range targets {
		newVal, stripped := stripPrefix(t.val, prefix)
		if !stripped {
			continue
		}
		upd := fmt.Sprintf(`UPDATE %s SET %s = ? WHERE %s = ?`, table, col, pkCol)
		if _, err := tx.Exec(upd, newVal, t.pk); err != nil {
			return fmt.Errorf("%s.%s update pk=%s: %w", table, col, t.pk, err)
		}
		m.updated++
	}
	return nil
}

func stripPrefix(s, prefix string) (string, bool) {
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):], true
	}
	return s, false
}

func stripBucketOrDBPrefix(name string) string {
	if strings.HasPrefix(name, "D-") {
		return name[2:]
	}
	if strings.HasPrefix(name, "bkt-B-") {
		return "bkt-" + name[len("bkt-B-"):]
	}
	return name
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=10000")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}
