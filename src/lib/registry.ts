import Database from "better-sqlite3";
import fs from "fs";
import path from "path";

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
  // Migrate existing installations — add column if missing
  const cols = _registry
    .prepare("PRAGMA table_info(databases)")
    .all() as { name: string }[];
  if (!cols.some((c) => c.name === "service_secret")) {
    _registry.exec("ALTER TABLE databases ADD COLUMN service_secret TEXT");
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
}

// Cached prepared statements — created once, reused across requests
let _stmts: {
  list: Database.Statement;
  get: Database.Statement;
  getBySecret: Database.Statement;
  insert: Database.Statement;
  softDelete: Database.Statement;
} | null = null;

function stmts() {
  if (_stmts) return _stmts;
  const db = getRegistry();
  _stmts = {
    list:        db.prepare("SELECT * FROM databases ORDER BY created_at DESC"),
    get:         db.prepare("SELECT * FROM databases WHERE name = ?"),
    getBySecret: db.prepare("SELECT * FROM databases WHERE service_secret = ? AND status = 'active'"),
    insert:      db.prepare("INSERT INTO databases (name, owner, description, service_secret) VALUES (?, ?, ?, ?)"),
    softDelete:  db.prepare("UPDATE databases SET status = 'deleted' WHERE name = ?"),
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
  stmts().softDelete.run(name);
}
