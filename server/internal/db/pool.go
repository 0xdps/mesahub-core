// Package db manages SQLite connection pools with WAL mode and a
// serialised write queue per database.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3" // CGO SQLite driver
	"github.com/rs/zerolog/log"
)

const pragmas = `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;
PRAGMA cache_size=-8000;
`

// Pool holds a single read-write connection per named database.
// Multiple goroutines share the same *sql.DB; serialisation for writes is
// handled by the write queue, not by holding a single connection.
type Pool struct {
	mu       sync.RWMutex
	dataPath string
	conns    map[string]*sql.DB
}

// NewPool creates a Pool rooted at dataPath.
func NewPool(dataPath string) *Pool {
	return &Pool{
		dataPath: dataPath,
		conns:    make(map[string]*sql.DB),
	}
}

// Get returns the open *sql.DB for name, opening it if necessary.
func (p *Pool) Get(name string) (*sql.DB, error) {
	p.mu.RLock()
	db, ok := p.conns[name]
	p.mu.RUnlock()
	if ok {
		return db, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock.
	if db, ok = p.conns[name]; ok {
		return db, nil
	}

	dbPath := filepath.Join(p.dataPath, name+".db")
	db, err := open(dbPath)
	if err != nil {
		return nil, err
	}
	p.conns[name] = db
	log.Debug().Str("db", name).Str("path", dbPath).Msg("opened db connection")
	return db, nil
}

// Close closes all open connections. Safe to call multiple times.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, db := range p.conns {
		if err := db.Close(); err != nil {
			log.Error().Err(err).Str("db", name).Msg("error closing db")
		}
		delete(p.conns, name)
	}
}

// Remove closes and deletes the database file for name.
func (p *Pool) Remove(name string) error {
	p.mu.Lock()
	if db, ok := p.conns[name]; ok {
		_ = db.Close()
		delete(p.conns, name)
	}
	p.mu.Unlock()

	dbPath := filepath.Join(p.dataPath, name+".db")
	walPath := dbPath + "-wal"
	shmPath := dbPath + "-shm"
	for _, f := range []string{dbPath, walPath, shmPath} {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("db: remove %s: %w", f, err)
		}
	}
	return nil
}

// open creates or opens the SQLite file at path and applies WAL pragmas.
func open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", path, err)
	}
	// Single writer; readers use WAL snapshots.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.Exec(pragmas); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: pragma setup for %s: %w", path, err)
	}
	return db, nil
}
