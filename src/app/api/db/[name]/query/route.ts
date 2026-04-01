import type { DatabaseHeader, DatabaseResultSet } from "@/drivers/base-driver";
import { authorizeDbRequest } from "@/lib/auth";
import { getReadonlyDbConnection } from "@/lib/db-pool";
import { logger } from "@/lib/logger";
import { getDatabase } from "@/lib/registry";
import { createHash } from "crypto";
import { NextResponse } from "next/server";

// Only allow read-only statements
const BLOCKED_PATTERN =
  /^\s*(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|ATTACH|DETACH|PRAGMA\s+\w+\s*=)/i;

interface Params {
  params: Promise<{ name: string }>;
}

function sqlFingerprint(sql: string): string {
  return createHash("sha256").update(sql).digest("hex").slice(0, 12);
}

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;

  const record = getDatabase(name);
  if (!record) {
    logger.warn(`[query] "${name}" — database not found`);
    return NextResponse.json({ error: "Database not found" }, { status: 404 });
  }

  const authError = authorizeDbRequest(req, record);
  if (authError) {
    logger.warn(`[query] "${name}" — auth rejected`);
    return authError;
  }

  const body = await req.json().catch(() => null);
  if (!body || typeof body.sql !== "string") {
    return NextResponse.json({ error: "sql is required" }, { status: 400 });
  }

  const { sql, bindings = [] } = body as { sql: string; bindings?: unknown[] };

  if (!Array.isArray(bindings)) {
    return NextResponse.json({ error: "bindings must be an array" }, { status: 400 });
  }

  if (BLOCKED_PATTERN.test(sql)) {
    logger.warn(`[query] "${name}" — write attempt blocked | sql_fp=${sqlFingerprint(sql)} len=${sql.length}`);
    return NextResponse.json(
      { error: "Only SELECT statements are allowed" },
      { status: 403 }
    );
  }

  const db = getReadonlyDbConnection(name);

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

    const rows = (stmt.all(...bindings) as Record<string, unknown>[]).map((row) => {
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
    logger.warn(
      `[query] "${name}" — SQL error: ${(err as Error).message} | sql_fp=${sqlFingerprint(sql)} len=${sql.length}`
    );
    return NextResponse.json(
      { error: (err as Error).message },
      { status: 400 }
    );
  }
}
