import Database from "better-sqlite3";
import fs from "fs";
import path from "path";
import { closeDbConnection } from "./db-pool";

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
