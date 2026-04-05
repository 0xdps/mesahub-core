import { authorizeDbRequest } from "@/lib/auth";
import { bulkDeleteFiles } from "@/lib/file-storage";
import { getDatabase, recordAuditEvent } from "@/lib/registry";
import { NextResponse } from "next/server";

export const runtime = "nodejs";
const MAX_BULK_DELETE_IDS = Number.parseInt(process.env.FILE_BULK_DELETE_MAX_IDS ?? "100", 10);

interface Params {
  params: Promise<{ name: string }>;
}

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;
  const record = getDatabase(name);
  if (!record) {
    return NextResponse.json({ error: "Database not found" }, { status: 404 });
  }

  const authError = authorizeDbRequest(req, record);
  if (authError) return authError;

  if (record.status !== "active") {
    return NextResponse.json({ error: "Database is inactive - writes are not allowed" }, { status: 403 });
  }

  const body = await req.json().catch(() => null);
  if (!body || typeof body !== "object" || !Array.isArray(body.file_ids)) {
    return NextResponse.json({ error: "file_ids must be an array" }, { status: 400 });
  }

  if (body.file_ids.length === 0) {
    return NextResponse.json({ error: "file_ids must be a non-empty array" }, { status: 400 });
  }

  if (body.file_ids.length > MAX_BULK_DELETE_IDS) {
    return NextResponse.json(
      { error: `Maximum ${MAX_BULK_DELETE_IDS} file IDs per request` },
      { status: 400 }
    );
  }

  const rawIds = body.file_ids as unknown[];
  const ids: string[] = Array.from(
    new Set(
      rawIds
        .filter((value): value is string => typeof value === "string" && value.trim().length > 0)
        .map((value) => value.trim())
    )
  );

  if (ids.length === 0) {
    return NextResponse.json({ error: "No valid file IDs provided" }, { status: 400 });
  }

  const result = bulkDeleteFiles(name, ids);

  recordAuditEvent({
    eventType: "file.bulk_delete",
    dbName: name,
    actor: req.headers.get("x-sqlite-hub-admin") === "1" ? "admin_session" : "service_secret",
    metadata: { requested: ids.length, deleted: result.deleted, failed: result.failed },
  });

  return NextResponse.json(result);
}
