import type { DatabaseHeader, DatabaseResultSet } from "@/drivers/base-driver";
import { getDbPath } from "@/lib/fs";
import { getDatabase } from "@/lib/registry";
import Database from "better-sqlite3";
import { NextResponse } from "next/server";

// Only allow read-only statements
const BLOCKED_PATTERN =
  /^\s*(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|ATTACH|DETACH|PRAGMA\s+\w+\s*=)/i;

interface Params {
  params: Promise<{ name: string }>;
}

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;

  const record = getDatabase(name);
  if (!record || record.status !== "active") {
    return NextResponse.json({ error: "Database not found" }, { status: 404 });
  }

  const body = await req.json().catch(() => null);
  if (!body || typeof body.sql !== "string") {
    return NextResponse.json({ error: "sql is required" }, { status: 400 });
  }

  const { sql } = body as { sql: string };

  if (BLOCKED_PATTERN.test(sql)) {
    return NextResponse.json(
      { error: "Only SELECT statements are allowed" },
      { status: 403 }
    );
  }

  const dbPath = getDbPath(name);
  const db = new Database(dbPath, { readonly: true, fileMustExist: true });

  try {
    const startTime = Date.now();
    const stmt = db.prepare(sql);
    const columnNames: string[] = stmt.columns().map((c) => c.name);

    const headers: DatabaseHeader[] = columnNames.map((colName) => ({
      name: colName,
      displayName: colName,
      originalType: null,
      type: undefined,
    }));

    const rows = (stmt.all() as Record<string, unknown>[]).map((row) => {
      const out: Record<string, unknown> = {};
      for (const h of headers) out[h.name] = row[h.name];
      return out;
    });

    const result: DatabaseResultSet = {
      headers,
      rows,
      stat: {
        rowsAffected: 0,
        rowsRead: rows.length,
        rowsWritten: null,
        queryDurationMs: Date.now() - startTime,
      },
    };

    return NextResponse.json(result);
  } catch (err) {
    return NextResponse.json(
      { error: (err as Error).message },
      { status: 400 }
    );
  } finally {
    db.close();
  }
}
