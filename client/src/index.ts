import pingpong from "@pingpong-js/fetch";

// ── Types ──────────────────────────────────────────────────────────────────

export interface ColumnHeader {
  name: string;
  displayName: string;
  originalType: string | null;
}

/** Returned when the SQL was a SELECT */
export interface QueryResult<T = Record<string, unknown>> {
  headers: ColumnHeader[];
  rows: T[];
  rowsRead: number;
}

/** Returned when the SQL was INSERT / UPDATE / DELETE / CREATE / etc. */
export interface ExecResult {
  rowsAffected: number;
  lastInsertRowid: number | null;
}

export type ExecOrQueryResult<T = Record<string, unknown>> =
  | QueryResult<T>
  | ExecResult;

export interface FileDbClientOptions {
  /** Base URL of your file-db deployment, e.g. https://my-app.up.railway.app */
  url: string;
  /** ADMIN_TOKEN configured on the file-db service */
  token: string;
  /** Name of the database to operate on */
  db: string;
  /** Request timeout in ms (default: 10 000) */
  timeout?: number;
}

// ── Client ─────────────────────────────────────────────────────────────────

export class FileDbClient {
  private readonly http: ReturnType<typeof pingpong.create>;
  private readonly dbName: string;

  constructor(options: FileDbClientOptions) {
    this.dbName = options.db;
    this.http = pingpong.create({
      baseURL: options.url.replace(/\/$/, ""),
      timeout: options.timeout ?? 10_000,
      headers: {
        Authorization: `Bearer ${options.token}`,
        "Content-Type": "application/json",
      },
    });
  }

  /**
   * Execute any SQL statement.
   * - SELECT → returns QueryResult with headers + rows
   * - INSERT / UPDATE / DELETE / CREATE / etc. → returns ExecResult
   */
  async exec<T = Record<string, unknown>>(
    sql: string,
    bindings?: unknown[]
  ): Promise<ExecOrQueryResult<T>> {
    const res = await this.http.post(
      `/api/db/${encodeURIComponent(this.dbName)}/exec`,
      { sql, ...(bindings?.length ? { bindings } : {}) }
    );

    if (res.isError()) {
      const body = res.data as { error?: string } | null;
      throw new Error(
        body?.error ?? `file-db: request failed with status ${res.status}`
      );
    }

    return res.data as ExecOrQueryResult<T>;
  }

  /**
   * Run a SELECT and return typed rows.
   * Throws if the SQL is not a reader statement.
   */
  async query<T = Record<string, unknown>>(
    sql: string,
    bindings?: unknown[]
  ): Promise<T[]> {
    const result = await this.exec<T>(sql, bindings);
    if (!("rows" in result)) {
      throw new Error("file-db: expected a SELECT statement, got a write result");
    }
    return result.rows;
  }

  /**
   * Run a SELECT and return the first row, or null if empty.
   */
  async queryOne<T = Record<string, unknown>>(
    sql: string,
    bindings?: unknown[]
  ): Promise<T | null> {
    const rows = await this.query<T>(sql, bindings);
    return rows[0] ?? null;
  }

  /**
   * Run a write statement (INSERT / UPDATE / DELETE / CREATE / ALTER / DROP).
   * Throws if the SQL is a SELECT.
   */
  async run(sql: string, bindings?: unknown[]): Promise<ExecResult> {
    const result = await this.exec(sql, bindings);
    if (!("rowsAffected" in result)) {
      throw new Error("file-db: expected a write statement, got a SELECT result");
    }
    return result;
  }
}

/**
 * Create a file-db client.
 *
 * @example
 * const db = createClient({
 *   url: process.env.FILE_DB_URL,
 *   token: process.env.FILE_DB_TOKEN,
 *   db: "my-service",
 * });
 *
 * await db.run(`CREATE TABLE IF NOT EXISTS users (
 *   id    INTEGER PRIMARY KEY AUTOINCREMENT,
 *   email TEXT NOT NULL UNIQUE,
 *   name  TEXT
 * )`);
 *
 * await db.run("INSERT INTO users (email, name) VALUES (?, ?)", ["a@b.com", "Alice"]);
 *
 * const users = await db.query<{ id: number; email: string }>("SELECT * FROM users");
 */
export function createClient(options: FileDbClientOptions): FileDbClient {
  return new FileDbClient(options);
}

export default createClient;
