import { authorizeDbRequest } from "@/lib/auth";
import { getFileById, isFileExpired } from "@/lib/file-storage";
import { getDatabase } from "@/lib/registry";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string; id: string }>;
}

export async function GET(req: Request, { params }: Params) {
  const { name, id } = await params;

  const record = getDatabase(name);
  if (!record) return NextResponse.json({ error: "Database not found" }, { status: 404 });

  const authError = authorizeDbRequest(req, record);
  if (authError) return authError;

  const file = getFileById(name, id);
  if (!file) return NextResponse.json({ error: "File not found" }, { status: 404 });
  if (isFileExpired(file)) return NextResponse.json({ error: "File expired" }, { status: 410 });

  return NextResponse.json({
    id: file.id,
    filename: file.filename,
    content_type: file.content_type,
    size_bytes: file.size_bytes,
    uploaded_at: file.uploaded_at,
    expires_at: file.expires_at,
    metadata: file.metadata ? JSON.parse(file.metadata) : null,
    content_hash: file.content_hash,
  });
}
