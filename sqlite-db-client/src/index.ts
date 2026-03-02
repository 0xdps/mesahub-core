export { Database } from "./database.js";
export type {
  WhereClause,
  FindOptions,
  ColumnDef,
  CreateTableOptions,
  CreateIndexOptions,
  OrderDirection,
} from "./database.js";

export { HttpAdapter } from "./adapters/http.js";
export type { HttpAdapterOptions } from "./adapters/http.js";
export type {
  IAdapter,
  ColumnHeader,
  QueryResult,
  ExecResult,
  RawResult,
} from "./adapters/types.js";

// ── Convenience factory ──────────────────────────────────────────────────────

import { Database } from "./database.js";
import { HttpAdapter, type HttpAdapterOptions } from "./adapters/http.js";

/**
 * Create a Database connected to a sqlite-db-hub service over HTTP.
 *
 * @example
 * const db = connect({
 *   url:   process.env.SQLITE_DB_HUB_URL,
 *   token: process.env.SQLITE_DB_HUB_TOKEN,
 *   db:    "my-service",
 * });
 */
export function connect(options: HttpAdapterOptions): Database {
  return new Database(new HttpAdapter(options));
}

export default connect;
