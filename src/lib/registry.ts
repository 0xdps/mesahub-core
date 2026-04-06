import Database from "better-sqlite3";
import fs from "fs";
import path from "path";
import { closeDbConnection } from "./db-pool";
import { deleteFilesForDatabase } from "./file-storage";

const DATA_PATH = process.env.DATA_PATH ?? "/data";

let _registry: Database.Database | null = null;

export function getRegistry(): Database.Database {
  if (_registry) return _registry;

  const dbPath = path.join(DATA_PATH, "registry.db");
  fs.mkdirSync(DATA_PATH, { recursive: true });
  _registry = new Database(dbPath);
  _registry.pragma("journal_mode = WAL");
  _registry.exec(`
    CREATE TABLE IF NOT EXISTS databases (
      id             INTEGER PRIMARY KEY AUTOINCREMENT,
      name           TEXT UNIQUE NOT NULL,
      owner          TEXT NOT NULL,
      description    TEXT,
      service_secret TEXT,
      created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
      status         TEXT DEFAULT 'active'
    );

    CREATE TABLE IF NOT EXISTS file_token_revocations (
      token_id   TEXT PRIMARY KEY,
      db_name    TEXT NOT NULL,
      expires_at DATETIME NOT NULL,
      reason     TEXT,
      revoked_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );

    CREATE INDEX IF NOT EXISTS idx_file_token_revocations_db_name ON file_token_revocations(db_name);
    CREATE INDEX IF NOT EXISTS idx_file_token_revocations_expires_at ON file_token_revocations(expires_at);

    CREATE TABLE IF NOT EXISTS audit_events (
      id         INTEGER PRIMARY KEY AUTOINCREMENT,
      event_type TEXT NOT NULL,
      db_name    TEXT,
      actor      TEXT,
      metadata   TEXT,
      created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );

    CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events(created_at);
    CREATE INDEX IF NOT EXISTS idx_audit_events_db_name ON audit_events(db_name);
    CREATE INDEX IF NOT EXISTS idx_audit_events_event_type ON audit_events(event_type);
  `);
  // Migrate existing installations — add columns if missing
  const cols = _registry
    .prepare("PRAGMA table_info(databases)")
    .all() as { name: string }[];
  if (!cols.some((c) => c.name === "service_secret")) {
    _registry.exec("ALTER TABLE databases ADD COLUMN service_secret TEXT");
  }
  if (!cols.some((c) => c.name === "original_name")) {
    _registry.exec("ALTER TABLE databases ADD COLUMN original_name TEXT");
  }
  if (!cols.some((c) => c.name === "deleted_at")) {
    _registry.exec("ALTER TABLE databases ADD COLUMN deleted_at DATETIME");
  }
  return _registry;
}

export interface DbRecord {
  id: number;
  name: string;
  owner: string;
  description: string | null;
  service_secret: string | null;
  created_at: string;
  status: string;
  original_name: string | null;
  deleted_at: string | null;
}

export interface AuditMetrics {
  totalEvents: number;
  eventsLast24h: number;
  byTypeLast24h: Record<string, number>;
}

// Cached prepared statements — created once, reused across requests
let _stmts: {
  list: Database.Statement;
  get: Database.Statement;
  getBySecret: Database.Statement;
  insert: Database.Statement;
  updateSecret: Database.Statement;
} | null = null;

function stmts() {
  if (_stmts) return _stmts;
  const db = getRegistry();
  _stmts = {
    list:         db.prepare("SELECT * FROM databases WHERE status != 'deleted' ORDER BY created_at DESC"),
    get:          db.prepare("SELECT * FROM databases WHERE name = ?"),
    getBySecret:  db.prepare("SELECT * FROM databases WHERE service_secret = ? AND status = 'active'"),
    insert:       db.prepare("INSERT INTO databases (name, owner, description, service_secret) VALUES (?, ?, ?, ?)"),
    updateSecret: db.prepare("UPDATE databases SET service_secret = ? WHERE name = ?"),
  };
  return _stmts;
}

export function listDatabases(): DbRecord[] {
  return stmts().list.all() as DbRecord[];
}

export function getDatabase(name: string): DbRecord | null {
  return (stmts().get.get(name) as DbRecord) ?? null;
}

export function getDbByServiceSecret(secret: string): DbRecord | null {
  return (stmts().getBySecret.get(secret) as DbRecord) ?? null;
}

export function insertDatabase(name: string, owner: string, description?: string, serviceSecret?: string): DbRecord {
  stmts().insert.run(name, owner, description ?? null, serviceSecret ?? null);
  return getDatabase(name)!;
}

export function softDeleteDatabase(name: string): void {
  const epoch = Math.floor(Date.now() / 1000);
  const newName = `${name}-${epoch}`;
  const oldPath = path.join(DATA_PATH, `${name}.db`);
  const newPath = path.join(DATA_PATH, `${newName}.db`);

  // Close open connections before renaming the file
  closeDbConnection(name);

  // Rename DB file + WAL/SHM sidecar files if they exist
  if (fs.existsSync(oldPath)) fs.renameSync(oldPath, newPath);
  if (fs.existsSync(oldPath + "-wal")) fs.renameSync(oldPath + "-wal", newPath + "-wal");
  if (fs.existsSync(oldPath + "-shm")) fs.renameSync(oldPath + "-shm", newPath + "-shm");

  // Remove files owned by this DB during soft-delete to avoid orphaned blobs.
  deleteFilesForDatabase(name);

  getRegistry()
    .prepare(
      "UPDATE databases SET name = ?, original_name = ?, status = 'deleted', deleted_at = datetime('now'), service_secret = NULL WHERE name = ?"
    )
    .run(newName, name, name);
}

export function listDeletedDatabases(): DbRecord[] {
  return getRegistry()
    .prepare("SELECT * FROM databases WHERE status = 'deleted' ORDER BY deleted_at DESC")
    .all() as DbRecord[];
}

export function restoreDatabase(deletedName: string): DbRecord {
  const record = getDatabase(deletedName);
  if (!record || record.status !== "deleted" || !record.original_name) {
    throw new Error("Database not found or not in deleted state");
  }

  const originalName = record.original_name;
  const oldPath = path.join(DATA_PATH, `${deletedName}.db`);
  const newPath = path.join(DATA_PATH, `${originalName}.db`);

  // Rename file back
  if (fs.existsSync(oldPath)) fs.renameSync(oldPath, newPath);
  if (fs.existsSync(oldPath + "-wal")) fs.renameSync(oldPath + "-wal", newPath + "-wal");
  if (fs.existsSync(oldPath + "-shm")) fs.renameSync(oldPath + "-shm", newPath + "-shm");

  getRegistry()
    .prepare(
      "UPDATE databases SET name = ?, original_name = NULL, status = 'active', deleted_at = NULL WHERE name = ?"
    )
    .run(originalName, deletedName);

  return getDatabase(originalName)!;
}

export function hardDeleteDatabase(deletedName: string): void {
  const record = getDatabase(deletedName);
  if (!record || record.status !== "deleted") {
    throw new Error("Database not found or not in deleted state");
  }

  const dbPath = path.join(DATA_PATH, `${deletedName}.db`);

  // Remove DB file + sidecar files
  if (fs.existsSync(dbPath)) fs.unlinkSync(dbPath);
  if (fs.existsSync(dbPath + "-wal")) fs.unlinkSync(dbPath + "-wal");
  if (fs.existsSync(dbPath + "-shm")) fs.unlinkSync(dbPath + "-shm");

  deleteFilesForDatabase(deletedName);

  getRegistry()
    .prepare("DELETE FROM databases WHERE name = ?")
    .run(deletedName);
}

export function updateServiceSecret(name: string, secret: string | null): void {
  stmts().updateSecret.run(secret, name);
}

export function setDatabaseStatus(name: string, status: "active" | "inactive"): void {
  getRegistry().prepare("UPDATE databases SET status = ? WHERE name = ?").run(status, name);
}

export function revokeFileToken(input: {
  tokenId: string;
  dbName: string;
  expiresAt: string;
  reason?: string;
}): void {
  getRegistry()
    .prepare(
      `INSERT INTO file_token_revocations (token_id, db_name, expires_at, reason)
       VALUES (?, ?, ?, ?)
       ON CONFLICT(token_id) DO UPDATE SET
         db_name = excluded.db_name,
         expires_at = excluded.expires_at,
         reason = excluded.reason,
         revoked_at = CURRENT_TIMESTAMP`
    )
    .run(input.tokenId, input.dbName, input.expiresAt, input.reason ?? null);
}

export function isFileTokenRevoked(tokenId: string, dbName: string): boolean {
  const row = getRegistry()
    .prepare(
      `SELECT token_id
       FROM file_token_revocations
       WHERE token_id = ? AND db_name = ? AND datetime(expires_at) > datetime('now')
       LIMIT 1`
    )
    .get(tokenId, dbName) as { token_id: string } | undefined;

  return !!row;
}

export function cleanupExpiredFileTokenRevocations(): { deleted: number } {
  const result = getRegistry()
    .prepare("DELETE FROM file_token_revocations WHERE datetime(expires_at) <= datetime('now')")
    .run();
  return { deleted: result.changes };
}

export function recordAuditEvent(input: {
  eventType: string;
  dbName?: string;
  actor?: string;
  metadata?: Record<string, unknown>;
}): void {
  getRegistry()
    .prepare("INSERT INTO audit_events (event_type, db_name, actor, metadata) VALUES (?, ?, ?, ?)")
    .run(
      input.eventType,
      input.dbName ?? null,
      input.actor ?? null,
      input.metadata ? JSON.stringify(input.metadata) : null
    );
}

export function getAuditMetrics(): AuditMetrics {
  const db = getRegistry();
  const totals = db
    .prepare("SELECT COUNT(*) as total, COALESCE(SUM(CASE WHEN datetime(created_at) >= datetime('now', '-1 day') THEN 1 ELSE 0 END), 0) as last24h FROM audit_events")
    .get() as { total: number; last24h: number };

  const byTypeRows = db
    .prepare(
      `SELECT event_type, COUNT(*) as count
       FROM audit_events
       WHERE datetime(created_at) >= datetime('now', '-1 day')
       GROUP BY event_type`
    )
    .all() as { event_type: string; count: number }[];

  const byTypeLast24h: Record<string, number> = {};
  for (const row of byTypeRows) {
    byTypeLast24h[row.event_type] = row.count;
  }

  return {
    totalEvents: totals.total,
    eventsLast24h: totals.last24h,
    byTypeLast24h,
  };
}
