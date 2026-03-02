/** A single column header returned by a SELECT */
export interface ColumnHeader {
  name: string;
  displayName: string;
  originalType: string | null;
}

/** Result of a SELECT statement */
export interface QueryResult<T = Record<string, unknown>> {
  headers: ColumnHeader[];
  rows: T[];
  rowsRead: number;
}

/** Result of INSERT / UPDATE / DELETE / DDL */
export interface ExecResult {
  rowsAffected: number;
  lastInsertRowid: number | null;
}

export type RawResult<T = Record<string, unknown>> =
  | QueryResult<T>
  | ExecResult;

/**
 * Minimal contract every adapter must fulfil.
 * Only `exec` is required — all high-level APIs are built on top of it.
 */
export interface IAdapter {
  /**
   * Execute a SQL statement with optional positional bindings.
   * Returns QueryResult for SELECT, ExecResult for everything else.
   */
  exec<T = Record<string, unknown>>(
    sql: string,
    bindings?: unknown[]
  ): Promise<RawResult<T>>;
}
