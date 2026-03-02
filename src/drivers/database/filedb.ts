import type { DatabaseResultSet, QueryableBaseDriver } from "@/drivers/base-driver";
import { SqliteLikeBaseDriver } from "@/drivers/sqlite-base-driver";

class FilebDbQueryable implements QueryableBaseDriver {
  constructor(private dbName: string) {}

  async query(stmt: string): Promise<DatabaseResultSet> {
    const res = await fetch(`/api/db/${encodeURIComponent(this.dbName)}/query`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ sql: stmt }),
    });

    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || `Query failed: ${res.status}`);
    }

    return res.json() as Promise<DatabaseResultSet>;
  }

  async transaction(stmts: string[]): Promise<DatabaseResultSet[]> {
    return Promise.all(stmts.map((s) => this.query(s)));
  }
}

export default class FilebDbDriver extends SqliteLikeBaseDriver {
  constructor(dbName: string) {
    super(new FilebDbQueryable(dbName));
  }
}
