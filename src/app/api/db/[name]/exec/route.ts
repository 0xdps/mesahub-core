import type { DatabaseHeader } from "@/drivers/base-driver";
import { authorizeDbRequest } from "@/lib/auth";
import { getDbConnection, runInImmediateTransaction } from "@/lib/db-pool";
import { recordExecError, recordExecRequest, recordExecSuccess } from "@/lib/exec-metrics";
import { logger } from "@/lib/logger";
import { getDatabase } from "@/lib/registry";
import { enqueueDbWrite, getWriteQueueDepth } from "@/lib/write-queue";
import { createHash } from "crypto";
import { NextResponse } from "next/server";

interface Params {
  params: Promise<{ name: string }>;
}

type SqlType = "read" | "write" | "unknown";

const MAX_SQL_LENGTH = Number.parseInt(process.env.MAX_SQL_LENGTH ?? "100000", 10);
const MAX_BINDINGS = Number.parseInt(process.env.MAX_SQL_BINDINGS ?? "5000", 10);

function classifySql(sql: string): SqlType {
  const trimmed = sql.trimStart();
  const keyword = trimmed.split(/\s+/, 1)[0]?.toUpperCase();
  if (!keyword) return "unknown";

  if (["SELECT", "WITH", "VALUES", "EXPLAIN"].includes(keyword)) {
    return "read";
  }

  if (keyword === "PRAGMA") {
    return /=/.test(trimmed) ? "write" : "read";
  }

  if (["INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "ATTACH", "DETACH", "REPLACE", "VACUUM"].includes(keyword)) {
    return "write";
  }

  return "unknown";
}

function sqlFingerprint(sql: string): string {
  return createHash("sha256").update(sql).digest("hex").slice(0, 12);
}

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;

  const record = getDatabase(name);
  if (!record) {
    logger.warn(`[exec] "${name}" — database not found`);
    return NextResponse.json({ error: "Database not found" }, { status: 404 });
  }

  const authError = authorizeDbRequest(req, record);
  if (authError) {
    logger.warn(`[exec] "${name}" — auth rejected`);
    return authError;
  }

  const body = await req.json().catch(() => null);
  if (!body || typeof body.sql !== "string") {
    return NextResponse.json({ error: "sql is required" }, { status: 400 });
  }

  const { sql, bindings = [] } = body as { sql: string; bindings?: unknown[] };
  const sqlType = classifySql(sql);

  if (sql.length > MAX_SQL_LENGTH) {
    return NextResponse.json(
      { error: `sql is too long (max ${MAX_SQL_LENGTH} characters)` },
      { status: 400 }
    );
  }

  if (!Array.isArray(bindings)) {
    return NextResponse.json({ error: "bindings must be an array" }, { status: 400 });
  }

  if (bindings.length > MAX_BINDINGS) {
    return NextResponse.json(
      { error: `too many bindings (max ${MAX_BINDINGS})` },
      { status: 400 }
    );
  }

  if (record.status !== "active" && sqlType === "write") {
    logger.warn(`[exec] "${name}" — write blocked (inactive): ${sql.slice(0, 80)}`);
    return NextResponse.json(
      { error: "Database is inactive — writes are not allowed" },
      { status: 403 }
    );
  }

  const db = getDbConnection(name);

  try {
    recordExecRequest(sqlType === "write" ? "write" : "read");

    const stmt = db.prepare(sql);

    if (stmt.reader && sqlType !== "write") {
      // SELECT-like statements — return rows + headers
      const startedAt = Date.now();
      const columns = stmt.columns();
      const headers: DatabaseHeader[] = columns.map((c) => ({
        name: c.name,
        displayName: c.name,
        originalType: c.type ?? null,
        type: undefined,
      }));
      const rows = stmt.all(...bindings) as Record<string, unknown>[];
      recordExecSuccess({ rowsRead: rows.length, executionMs: Date.now() - startedAt });
      return NextResponse.json({ headers, rows, rowsRead: rows.length });
    } else if (stmt.reader) {
      // Write statements with RETURNING must be queued, but still return rows.
      const queuedAt = Date.now();
      const rows = await enqueueDbWrite(name, () => {
        const queueWaitMs = Date.now() - queuedAt;
        const startedAt = Date.now();
        const resultRows = runInImmediateTransaction(name, () => stmt.all(...bindings) as Record<string, unknown>[]);
        recordExecSuccess({
          rowsRead: resultRows.length,
          queueWaitMs,
          executionMs: Date.now() - startedAt,
        });
        if (queueWaitMs > 1000) {
          logger.warn(`[exec] "${name}" — write queue wait ${queueWaitMs}ms (depth=${getWriteQueueDepth(name)})`);
        }
        return resultRows;
      });

      const columns = stmt.columns();
      const headers: DatabaseHeader[] = columns.map((c) => ({
        name: c.name,
        displayName: c.name,
        originalType: c.type ?? null,
        type: undefined,
      }));

      return NextResponse.json({ headers, rows, rowsRead: rows.length });
    } else {
      // INSERT / UPDATE / DELETE / CREATE / DROP / ALTER etc. (no RETURNING)
      const queuedAt = Date.now();
      const info = await enqueueDbWrite(name, () => {
        const queueWaitMs = Date.now() - queuedAt;
        const startedAt = Date.now();
        const result = runInImmediateTransaction(name, () => stmt.run(...bindings));
        recordExecSuccess({
          rowsAffected: result.changes,
          queueWaitMs,
          executionMs: Date.now() - startedAt,
        });
        if (queueWaitMs > 1000) {
          logger.warn(`[exec] "${name}" — write queue wait ${queueWaitMs}ms (depth=${getWriteQueueDepth(name)})`);
        }
        return result;
      });

      return NextResponse.json({
        rowsAffected: info.changes,
        lastInsertRowid:
          typeof info.lastInsertRowid === "bigint"
            ? Number(info.lastInsertRowid)
            : info.lastInsertRowid,
      });
    }
  } catch (err) {
    const message = (err as Error).message;
    recordExecError(message);
    logger.warn(
      `[exec] "${name}" — SQL error: ${message} | sql_fp=${sqlFingerprint(sql)} len=${sql.length} type=${sqlType}`
    );
    return NextResponse.json(
      { error: message },
      { status: 400 }
    );
  }
}
