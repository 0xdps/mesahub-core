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
      id          INTEGER PRIMARY KEY AUTOINCREMENT,
      name        TEXT UNIQUE NOT NULL,
      owner       TEXT NOT NULL,
      description TEXT,
      created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
      status      TEXT DEFAULT 'active'
    );
  `);
  return _registry;
}

export interface DbRecord {
  id: number;
  name: string;
  owner: string;
  description: string | null;
  created_at: string;
  status: string;
}

export function listDatabases(): DbRecord[] {
  return getRegistry().prepare("SELECT * FROM databases ORDER BY created_at DESC").all() as DbRecord[];
}

export function getDatabase(name: string): DbRecord | null {
  return (getRegistry().prepare("SELECT * FROM databases WHERE name = ?").get(name) as DbRecord) ?? null;
}

export function insertDatabase(name: string, owner: string, description?: string): DbRecord {
  const db = getRegistry();
  db.prepare("INSERT INTO databases (name, owner, description) VALUES (?, ?, ?)").run(name, owner, description ?? null);
  return getDatabase(name)!;
}

export function softDeleteDatabase(name: string): void {
  getRegistry().prepare("UPDATE databases SET status = 'deleted' WHERE name = ?").run(name);
}
