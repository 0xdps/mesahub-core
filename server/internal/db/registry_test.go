package db_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdps/sqlite-hub/server/internal/db"
)

// openTestRegistry opens a fresh registry in t.TempDir() so each test is isolated.
func openTestRegistry(t *testing.T) *db.Registry {
	t.Helper()
	dir := t.TempDir()
	reg, err := db.OpenRegistry(dir)
	if err != nil {
		t.Fatalf("OpenRegistry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	return reg
}

// ── basic CRUD ────────────────────────────────────────────────────────────────

func TestInsertAndGetDatabase(t *testing.T) {
	reg := openTestRegistry(t)

	desc := "A test db"
	rec, err := reg.InsertDatabase("mydb", "alice", &desc)
	if err != nil {
		t.Fatalf("InsertDatabase: %v", err)
	}
	if rec == nil {
		t.Fatal("expected record, got nil")
	}
	if rec.Name != "mydb" {
		t.Errorf("Name = %q; want %q", rec.Name, "mydb")
	}
	if rec.Owner != "alice" {
		t.Errorf("Owner = %q; want %q", rec.Owner, "alice")
	}
	if rec.Status != "active" {
		t.Errorf("Status = %q; want active", rec.Status)
	}
}

func TestGetDatabaseNotFound(t *testing.T) {
	reg := openTestRegistry(t)
	got, err := reg.GetDatabase("nope")
	if err != nil {
		t.Fatalf("GetDatabase: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestListDatabases(t *testing.T) {
	reg := openTestRegistry(t)

	for _, name := range []string{"db1", "db2", "db3"} {
		if _, err := reg.InsertDatabase(name, "bob", nil); err != nil {
			t.Fatalf("InsertDatabase(%s): %v", name, err)
		}
	}

	all, err := reg.ListDatabases()
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len = %d; want 3", len(all))
	}
}

func TestSetDatabaseStatus(t *testing.T) {
	reg := openTestRegistry(t)
	if _, err := reg.InsertDatabase("statusdb", "carol", nil); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetDatabaseStatus("statusdb", "inactive"); err != nil {
		t.Fatalf("SetDatabaseStatus: %v", err)
	}
	rec, err := reg.GetDatabase("statusdb")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != "inactive" {
		t.Errorf("Status = %q; want inactive", rec.Status)
	}
}

// ── soft delete / restore ─────────────────────────────────────────────────────

func TestSoftDeleteAndRestore(t *testing.T) {
	dir := t.TempDir()
	reg, err := db.OpenRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	// Create a dummy sqlite file so SoftDelete can rename it.
	dbFile := filepath.Join(dir, "softdb.db")
	if err := touchFile(dbFile); err != nil {
		t.Fatal(err)
	}

	pool := db.NewPool(dir)
	t.Cleanup(func() { pool.Close() })

	if _, err := reg.InsertDatabase("softdb", "eve", nil); err != nil {
		t.Fatal(err)
	}

	if err := reg.SoftDeleteDatabase(pool, "softdb"); err != nil {
		t.Fatalf("SoftDeleteDatabase: %v", err)
	}

	// Should not appear in the normal listing.
	all, err := reg.ListDatabases()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range all {
		if r.Name == "softdb" {
			t.Fatal("softdb still appears in ListDatabases after delete")
		}
	}

	// Find the deleted record so we know its renamed key.
	deleted, err := reg.ListDeletedDatabases()
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted db, got %d", len(deleted))
	}
	deletedName := deleted[0].Name

	// Restore it.
	if _, err := reg.RestoreDatabase(pool, deletedName); err != nil {
		t.Fatalf("RestoreDatabase: %v", err)
	}
	rec, err := reg.GetDatabase("softdb")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("softdb not found after restore")
	}
	if rec.Status != "active" {
		t.Errorf("Status = %q; want active", rec.Status)
	}
}

// ── hard delete ───────────────────────────────────────────────────────────────

func TestHardDeleteDatabase(t *testing.T) {
	dir := t.TempDir()
	reg, err := db.OpenRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	if _, err := reg.InsertDatabase("harddb", "frank", nil); err != nil {
		t.Fatal(err)
	}

	// Soft-delete first so HardDelete can find the record in 'deleted' state.
	dbFile := filepath.Join(dir, "harddb.db")
	if err := touchFile(dbFile); err != nil {
		t.Fatal(err)
	}
	pool := db.NewPool(dir)
	t.Cleanup(func() { pool.Close() })

	if err := reg.SoftDeleteDatabase(pool, "harddb"); err != nil {
		t.Fatal(err)
	}

	deleted, err := reg.ListDeletedDatabases()
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) == 0 {
		t.Fatal("no deleted databases found")
	}
	deletedName := deleted[0].Name

	if err := reg.HardDeleteDatabase(deletedName); err != nil {
		t.Fatalf("HardDeleteDatabase: %v", err)
	}
	rec, err := reg.GetDatabase(deletedName)
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Errorf("record still present after hard delete: %+v", rec)
	}
}

// ── file token revocations ────────────────────────────────────────────────────

func TestFileTokenRevocation(t *testing.T) {
	reg := openTestRegistry(t)

	reason := "manual"
	if err := reg.RevokeFileToken("tok1", "mydb", "2030-01-01 00:00:00", &reason); err != nil {
		t.Fatalf("RevokeFileToken: %v", err)
	}

	revoked, err := reg.IsFileTokenRevoked("tok1", "mydb")
	if err != nil {
		t.Fatalf("IsFileTokenRevoked: %v", err)
	}
	if !revoked {
		t.Error("expected token tok1 to be revoked")
	}

	// Unknown token is not revoked.
	ok, err := reg.IsFileTokenRevoked("unknown", "mydb")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("unknown token should not be revoked")
	}
}

func TestCleanupExpiredRevocations(t *testing.T) {
	reg := openTestRegistry(t)

	reason := "test"
	// Insert one already-expired token (expires_at in the past).
	if err := reg.RevokeFileToken("expired", "db1", "2000-01-01 00:00:00", &reason); err != nil {
		t.Fatal(err)
	}
	// Insert one valid token.
	if err := reg.RevokeFileToken("valid", "db1", "2099-01-01 00:00:00", &reason); err != nil {
		t.Fatal(err)
	}

	removed, err := reg.CleanupExpiredTokenRevocations()
	if err != nil {
		t.Fatalf("CleanupExpiredTokenRevocations: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d; want 1", removed)
	}

	// Expired token should now be gone; valid one remains.
	e, _ := reg.IsFileTokenRevoked("expired", "db1")
	if e {
		t.Error("expired token still present after cleanup")
	}
	v, _ := reg.IsFileTokenRevoked("valid", "db1")
	if !v {
		t.Error("valid token removed by cleanup — should not have been")
	}
}

// ── audit events ──────────────────────────────────────────────────────────────

func TestAuditEvents(t *testing.T) {
	reg := openTestRegistry(t)

	dbName := "mydb"
	actor := "alice"
	for i := 0; i < 5; i++ {
		if err := reg.RecordAuditEvent("db.query", &dbName, &actor, map[string]any{"q": i}); err != nil {
			t.Fatalf("RecordAuditEvent %d: %v", i, err)
		}
	}

	metrics, err := reg.GetAuditMetrics()
	if err != nil {
		t.Fatalf("GetAuditMetrics: %v", err)
	}
	if metrics.TotalEvents < 5 {
		t.Errorf("TotalEvents = %d; want >= 5", metrics.TotalEvents)
	}
	if metrics.EventsLast24h < 5 {
		t.Errorf("EventsLast24h = %d; want >= 5", metrics.EventsLast24h)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func touchFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}
