import Database from "better-sqlite3";
import path from "path";

const DATA_PATH = process.env.DATA_PATH ?? "/data";

let _registry: Database.Database | null = null;

export function getRegistry(): Database.Database {
  if (_registry) return _registry;

  const dbPath = path.join(DATA_PATH, "registry.db");
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

export function listDatabases(): DbRecord[] {
  return getRegistry().prepare("SELECT * FROM databases ORDER BY created_at DESC").all() as DbRecord[];
}

export function getDatabase(name: string): DbRecord | null {
  return (getRegistry().prepare("SELECT * FROM databases WHERE name = ?").get(name) as DbRecord) ?? null;
}

export function getDbByServiceSecret(secret: string): DbRecord | null {
  return (getRegistry().prepare("SELECT * FROM databases WHERE service_secret = ? AND status = 'active'").get(secret) as DbRecord) ?? null;
}

export function insertDatabase(name: string, owner: string, description?: string, serviceSecret?: string): DbRecord {
  const db = getRegistry();
  db.prepare("INSERT INTO databases (name, owner, description, service_secret) VALUES (?, ?, ?, ?)").run(name, owner, description ?? null, serviceSecret ?? null);
  return getDatabase(name)!;
}

export function softDeleteDatabase(name: string): void {
  getRegistry().prepare("UPDATE databases SET status = 'deleted' WHERE name = ?").run(name);
}
