/**
 * Module-level connection pool for user databases.
 * better-sqlite3 is synchronous — keeping connections open avoids the overhead
 * of open/close on every request, which was the main source of latency.
 *
 * Connections are keyed by DB name. In Next.js server (Node.js process) the
 * module is long-lived, so the pool persists across requests.
 */

import Database from "better-sqlite3";
import { getDbPath } from "./fs";

const _pool = new Map<string, Database.Database>();
const _readonlyPool = new Map<string, Database.Database>();

export function getDbConnection(name: string): Database.Database {
  let db = _pool.get(name);
  if (!db || !db.open) {
    db = new Database(getDbPath(name));
    db.pragma("journal_mode = WAL");
    db.pragma("busy_timeout = 5000");
    _pool.set(name, db);
  }
  return db;
}

export function getReadonlyDbConnection(name: string): Database.Database {
  let db = _readonlyPool.get(name);
  if (!db || !db.open) {
    db = new Database(getDbPath(name), { readonly: true, fileMustExist: true });
    db.pragma("busy_timeout = 5000");
    _readonlyPool.set(name, db);
  }
  return db;
}

export function closeDbConnection(name: string): void {
  const rw = _pool.get(name);
  if (rw?.open) rw.close();
  _pool.delete(name);

  const ro = _readonlyPool.get(name);
  if (ro?.open) ro.close();
  _readonlyPool.delete(name);
}
