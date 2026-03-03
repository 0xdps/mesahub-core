import type { ExecResult, IAdapter, QueryResult, RawResult } from "./adapters/types.js";

// ── Helper types ────────────────────────────────────────────────────────────

export type WhereClause = Record<string, unknown>;
export type OrderDirection = "ASC" | "DESC";

export interface FindOptions {
  /** Columns to SELECT (default: all) */
  columns?: string[];
  orderBy?: string;
  order?: OrderDirection;
  limit?: number;
  offset?: number;
}

export interface ColumnDef {
  name: string;
  type: "INTEGER" | "TEXT" | "REAL" | "BLOB" | "NUMERIC" | string;
  primaryKey?: boolean;
  autoIncrement?: boolean;
  notNull?: boolean;
  unique?: boolean;
  default?: string | number;
}

export interface CreateTableOptions {
  ifNotExists?: boolean;
}

export interface CreateIndexOptions {
  unique?: boolean;
  ifNotExists?: boolean;
}

// ── SQL builder helpers ─────────────────────────────────────────────────────

function buildWhere(
  where: WhereClause
): { clause: string; bindings: unknown[] } {
  const keys = Object.keys(where);
  if (keys.length === 0) return { clause: "", bindings: [] };
  const parts = keys.map((k) => `"${k}" = ?`);
  return {
    clause: " WHERE " + parts.join(" AND "),
    bindings: keys.map((k) => where[k]),
  };
}

function isQueryResult<T>(
  r: RawResult<T>
): r is QueryResult<T> {
  return "rows" in r;
}

function isExecResult(r: RawResult | null | undefined): r is ExecResult {
  return r != null && "rowsAffected" in r;
}

// ── Database ─────────────────────────────────────────────────────────────────

/**
 * High-level database client.
 * Accepts any `IAdapter` — currently `HttpAdapter`, extensible to direct
 * SQLite (via better-sqlite3 or sql.js) without changing business code.
 */
export class Database {
  constructor(private readonly adapter: IAdapter) {}

  // ── Schema ──────────────────────────────────────────────────────────────

  /**
   * Create a table.
   *
   * @example
   * await db.createTable("users", [
   *   { name: "id",    type: "INTEGER", primaryKey: true, autoIncrement: true },
   *   { name: "email", type: "TEXT",    notNull: true, unique: true },
   *   { name: "name",  type: "TEXT" },
   * ]);
   */
  async createTable(
    table: string,
    columns: ColumnDef[],
    options: CreateTableOptions = {}
  ): Promise<ExecResult> {
    const ifNotExists = options.ifNotExists !== false ? "IF NOT EXISTS" : "";
    const cols = columns.map((c) => {
      let def = `"${c.name}" ${c.type}`;
      if (c.primaryKey) def += " PRIMARY KEY";
      if (c.autoIncrement) def += " AUTOINCREMENT";
      if (c.notNull) def += " NOT NULL";
      if (c.unique) def += " UNIQUE";
      if (c.default !== undefined) def += ` DEFAULT ${c.default}`;
      return def;
    });
    const sql = `CREATE TABLE ${ifNotExists} "${table}" (${cols.join(", ")})`;
    return this._write(sql);
  }

  /**
   * Drop a table.
   */
  async dropTable(table: string, ifExists = true): Promise<ExecResult> {
    const ie = ifExists ? "IF EXISTS" : "";
    return this._write(`DROP TABLE ${ie} "${table}"`);
  }

  /**
   * Create an index on one or more columns.
   *
   * @example
   * await db.createIndex("idx_users_email", "users", ["email"], { unique: true });
   */
  async createIndex(
    indexName: string,
    table: string,
    columns: string[],
    options: CreateIndexOptions = {}
  ): Promise<ExecResult> {
    const unique = options.unique ? "UNIQUE" : "";
    const ifNotExists = options.ifNotExists !== false ? "IF NOT EXISTS" : "";
    const cols = columns.map((c) => `"${c}"`).join(", ");
    const sql = `CREATE ${unique} INDEX ${ifNotExists} "${indexName}" ON "${table}" (${cols})`;
    return this._write(sql);
  }

  /**
   * Drop an index.
   */
  async dropIndex(indexName: string, ifExists = true): Promise<ExecResult> {
    const ie = ifExists ? "IF EXISTS" : "";
    return this._write(`DROP INDEX ${ie} "${indexName}"`);
  }

  // ── Write ────────────────────────────────────────────────────────────────

  /**
   * Insert a single row.  Returns `{ rowsAffected, lastInsertRowid }`.
   *
   * @example
   * const { lastInsertRowid } = await db.insert("users", { email: "a@b.com", name: "Alice" });
   */
  async insert(
    table: string,
    data: Record<string, unknown>
  ): Promise<ExecResult> {
    const keys = Object.keys(data);
    const cols = keys.map((k) => `"${k}"`).join(", ");
    const placeholders = keys.map(() => "?").join(", ");
    const bindings = keys.map((k) => data[k]);
    return this._write(
      `INSERT INTO "${table}" (${cols}) VALUES (${placeholders})`,
      bindings
    );
  }

  /**
   * Insert multiple rows in a single transaction.
   *
   * @example
   * await db.insertMany("users", [
   *   { email: "a@b.com", name: "Alice" },
   *   { email: "b@c.com", name: "Bob" },
   * ]);
   */
  async insertMany(
    table: string,
    rows: Record<string, unknown>[]
  ): Promise<ExecResult> {
    if (rows.length === 0) return { rowsAffected: 0, lastInsertRowid: null };
    const keys = Object.keys(rows[0]);
    const cols = keys.map((k) => `"${k}"`).join(", ");
    const rowPlaceholders = rows.map(() => `(${keys.map(() => "?").join(", ")})`).join(", ");
    const bindings = rows.flatMap((row) => keys.map((k) => row[k]));
    return this._write(
      `INSERT INTO "${table}" (${cols}) VALUES ${rowPlaceholders}`,
      bindings
    );
  }

  /**
   * Update rows matching `where`.
   *
   * @example
   * await db.update("users", { name: "Bob" }, { id: 1 });
   */
  async update(
    table: string,
    data: Record<string, unknown>,
    where: WhereClause
  ): Promise<ExecResult> {
    const setKeys = Object.keys(data);
    const setClauses = setKeys.map((k) => `"${k}" = ?`).join(", ");
    const setBindings = setKeys.map((k) => data[k]);
    const { clause, bindings: whereBindings } = buildWhere(where);
    return this._write(
      `UPDATE "${table}" SET ${setClauses}${clause}`,
      [...setBindings, ...whereBindings]
    );
  }

  /**
   * Delete rows matching `where`.
   *
   * @example
   * await db.delete("users", { id: 1 });
   */
  async delete(table: string, where: WhereClause): Promise<ExecResult> {
    const { clause, bindings } = buildWhere(where);
    return this._write(`DELETE FROM "${table}"${clause}`, bindings);
  }

  // ── Read ─────────────────────────────────────────────────────────────────

  /**
   * Find rows with optional filtering, ordering, and pagination.
   *
   * @example
   * const users = await db.find<User>("users", { active: 1 }, {
   *   columns: ["id", "email"],
   *   orderBy: "created_at",
   *   order: "DESC",
   *   limit: 20,
   *   offset: 0,
   * });
   */
  async find<T = Record<string, unknown>>(
    table: string,
    where: WhereClause = {},
    options: FindOptions = {}
  ): Promise<T[]> {
    const cols =
      options.columns?.map((c) => `"${c}"`).join(", ") ?? "*";
    const { clause, bindings } = buildWhere(where);
    let sql = `SELECT ${cols} FROM "${table}"${clause}`;
    if (options.orderBy)
      sql += ` ORDER BY "${options.orderBy}" ${options.order ?? "ASC"}`;
    if (options.limit !== undefined) sql += ` LIMIT ${options.limit}`;
    if (options.offset !== undefined) sql += ` OFFSET ${options.offset}`;
    return this._read<T>(sql, bindings);
  }

  /**
   * Find the first row matching `where`, or `null`.
   *
   * @example
   * const user = await db.findOne<User>("users", { email: "a@b.com" });
   */
  async findOne<T = Record<string, unknown>>(
    table: string,
    where: WhereClause = {}
  ): Promise<T | null> {
    const rows = await this.find<T>(table, where, { limit: 1 });
    return rows[0] ?? null;
  }

  /**
   * Find a row by its primary key value (column `id` by default).
   *
   * @example
   * const user = await db.findById<User>("users", 42);
   */
  async findById<T = Record<string, unknown>>(
    table: string,
    id: unknown,
    idColumn = "id"
  ): Promise<T | null> {
    return this.findOne<T>(table, { [idColumn]: id });
  }

  /**
   * Count rows matching `where`.
   *
   * @example
   * const total = await db.count("users");
   * const active = await db.count("users", { active: 1 });
   */
  async count(table: string, where: WhereClause = {}): Promise<number> {
    const { clause, bindings } = buildWhere(where);
    const sql = `SELECT COUNT(*) AS n FROM "${table}"${clause}`;
    const rows = await this._read<{ n: number }>(sql, bindings);
    return rows[0]?.n ?? 0;
  }

  /**
   * Check whether any row matching `where` exists.
   */
  async exists(table: string, where: WhereClause): Promise<boolean> {
    return (await this.count(table, where)) > 0;
  }

  // ── Raw escape hatch ─────────────────────────────────────────────────────

  /**
   * Run any raw SQL statement.
   *
   * @example
   * const result = await db.exec("PRAGMA table_info(users)");
   */
  async exec<T = Record<string, unknown>>(
    sql: string,
    bindings?: unknown[]
  ): Promise<RawResult<T>> {
    return this.adapter.exec<T>(sql, bindings);
  }

  // ── Internal ─────────────────────────────────────────────────────────────

  private async _read<T>(sql: string, bindings?: unknown[]): Promise<T[]> {
    const result = await this.adapter.exec<T>(sql, bindings);
    if (!isQueryResult(result)) {
      throw new Error(
        `sqlite-hub: expected SELECT, got write result for: ${sql}`
      );
    }
    return result.rows;
  }

  private async _write(
    sql: string,
    bindings?: unknown[]
  ): Promise<ExecResult> {
    const result = await this.adapter.exec(sql, bindings);
    if (!isExecResult(result)) {
      throw new Error(
        `sqlite-hub: expected write result, got SELECT for: ${sql}`
      );
    }
    return result;
  }
}
