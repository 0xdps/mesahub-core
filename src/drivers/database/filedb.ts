import type { DatabaseResultSet, QueryableBaseDriver } from "@/drivers/base-driver";
import { SqliteLikeBaseDriver } from "@/drivers/sqlite-base-driver";

const WRITE_PATTERN =
  /^\s*(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|ATTACH|DETACH|PRAGMA\s+\w+\s*=)/i;

class FilebDbQueryable implements QueryableBaseDriver {
  constructor(private dbName: string) {}

  async query(stmt: string): Promise<DatabaseResultSet> {
    // Route write / DDL statements through the exec endpoint
    if (WRITE_PATTERN.test(stmt)) {
      return this.exec(stmt);
    }

    const res = await fetch(`/api/db/${encodeURIComponent(this.dbName)}/query`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ sql: stmt }),
    });

    if (!res.ok) {
      const data = await res.json().catch(() => null);
      throw new Error(data?.error || `Query failed: ${res.status}`);
    }

    return res.json() as Promise<DatabaseResultSet>;
  }

  async exec(stmt: string): Promise<DatabaseResultSet> {
    const res = await fetch(`/api/db/${encodeURIComponent(this.dbName)}/exec`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ sql: stmt }),
    });

    if (!res.ok) {
      const data = await res.json().catch(() => null);
      throw new Error(data?.error || `Exec failed: ${res.status}`);
    }

    const data = await res.json();

    // Exec may return a SELECT result (reader path) or a write result
    if (Array.isArray(data.rows)) {
      return data as DatabaseResultSet;
    }

    return {
      rows: [],
      headers: [],
      stat: {
        rowsAffected: data.rowsAffected ?? 0,
        rowsRead: null,
        rowsWritten: data.rowsAffected ?? null,
        queryDurationMs: null,
      },
      lastInsertRowid: data.lastInsertRowid ?? undefined,
    };
  }

  async transaction(stmts: string[]): Promise<DatabaseResultSet[]> {
    const results: DatabaseResultSet[] = [];
    for (const s of stmts) {
      results.push(await this.exec(s));
    }
    return results;
  }
}

export default class FilebDbDriver extends SqliteLikeBaseDriver {
  constructor(dbName: string) {
    super(new FilebDbQueryable(dbName));
  }
}
