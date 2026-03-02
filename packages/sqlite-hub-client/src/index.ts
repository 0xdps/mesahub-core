export { Database } from "./database.js";
export type {
  ColumnDef, CreateIndexOptions, CreateTableOptions, FindOptions, OrderDirection, WhereClause
} from "./database.js";

export { HttpAdapter } from "./adapters/http.js";
export type { HttpAdapterOptions } from "./adapters/http.js";
export type {
  ColumnHeader, ExecResult, IAdapter, QueryResult, RawResult
} from "./adapters/types.js";

// ── Convenience factory ──────────────────────────────────────────────────────

import { HttpAdapter, type HttpAdapterOptions } from "./adapters/http.js";
import { Database } from "./database.js";

/**
 * Create a Database connected to a sqlite-hub service over HTTP.
 *
 * @example
 * const db = connect({
 *   url:   process.env.SQLITE_HUB_URL,
 *   token: process.env.SQLITE_HUB_TOKEN,
 *   db:    "my-service",
 * });
 */
export function connect(options: HttpAdapterOptions): Database {
  return new Database(new HttpAdapter(options));
}

export default connect;
