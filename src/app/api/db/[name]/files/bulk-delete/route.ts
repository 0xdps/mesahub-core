import { authorizeDbRequest } from "@/lib/auth";
import { bulkDeleteFiles } from "@/lib/file-storage";
import { getDatabase } from "@/lib/registry";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

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
  if (!body || !Array.isArray(body.file_ids)) {
    return NextResponse.json({ error: "file_ids must be an array" }, { status: 400 });
  }

  const ids = body.file_ids.filter((value: unknown) => typeof value === "string" && value.trim().length > 0);
  const result = bulkDeleteFiles(name, ids);
  return NextResponse.json(result);
}
