import type { DatabaseResultSet, QueryableBaseDriver } from "@/drivers/base-driver";
import { SqliteLikeBaseDriver } from "@/drivers/sqlite-base-driver";

/**
 * Read-only driver for system databases (registry, control).
 * Points at /api/system/db/{name}/query — write attempts are silently rejected
 * by the server, but we also block them client-side for clarity.
 */
class SystemDbQueryable implements QueryableBaseDriver {
  constructor(private dbName: string) {}

  async query(stmt: string): Promise<DatabaseResultSet> {
    const res = await fetch(`/api/system/db/${encodeURIComponent(this.dbName)}/query`, {
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
    // System databases are read-only — route everything through query.
    return this.query(stmt);
  }

  async transaction(stmts: string[]): Promise<DatabaseResultSet[]> {
    const results: DatabaseResultSet[] = [];
    for (const s of stmts) {
      results.push(await this.query(s));
    }
    return results;
  }
}

export default class SystemDbDriver extends SqliteLikeBaseDriver {
  constructor(dbName: string) {
    super(new SystemDbQueryable(dbName));
  }
}
