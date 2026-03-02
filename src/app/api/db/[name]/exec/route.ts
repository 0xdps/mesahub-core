import type { DatabaseHeader } from "@/drivers/base-driver";
import { authorizeDbRequest } from "@/lib/auth";
import { getDbConnection } from "@/lib/db-pool";
import { getDatabase } from "@/lib/registry";
import { NextResponse } from "next/server";

interface Params {
  params: Promise<{ name: string }>;
}

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;

  const record = getDatabase(name);
  if (!record || record.status !== "active") {
    return NextResponse.json({ error: "Database not found" }, { status: 404 });
  }

  const authError = authorizeDbRequest(req, record);
  if (authError) return authError;

  const body = await req.json().catch(() => null);
  if (!body || typeof body.sql !== "string") {
    return NextResponse.json({ error: "sql is required" }, { status: 400 });
  }

  const { sql, bindings = [] } = body as { sql: string; bindings?: unknown[] };

  const db = getDbConnection(name);

  try {
    const stmt = db.prepare(sql);

    if (stmt.reader) {
      // SELECT — return rows + headers
      const columns = stmt.columns();
      const headers: DatabaseHeader[] = columns.map((c) => ({
        name: c.name,
        displayName: c.name,
        originalType: c.type ?? null,
        type: undefined,
      }));
      const rows = stmt.all(...bindings) as Record<string, unknown>[];
      return NextResponse.json({ headers, rows, rowsRead: rows.length });
    } else {
      // INSERT / UPDATE / DELETE / CREATE / DROP / ALTER etc.
      const info = stmt.run(...bindings);
      return NextResponse.json({
        rowsAffected: info.changes,
        lastInsertRowid:
          typeof info.lastInsertRowid === "bigint"
            ? Number(info.lastInsertRowid)
            : info.lastInsertRowid,
      });
    }
  } catch (err) {
    return NextResponse.json(
      { error: (err as Error).message },
      { status: 400 }
    );
  }
}
