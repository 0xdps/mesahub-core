import { authorizeDbRequest } from "@/lib/auth";
import { createPresignedFileUrl, type FileUrlDisposition } from "@/lib/file-url-signing";
import { getFileById, isFileExpired } from "@/lib/file-storage";
import { getDatabase } from "@/lib/registry";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string }>;
}

function normalizeOrigin(req: Request): string {
  const url = new URL(req.url);
  const proto = req.headers.get("x-forwarded-proto") ?? url.protocol.replace(":", "");
  const host = req.headers.get("x-forwarded-host") ?? req.headers.get("host") ?? url.host;
  return `${proto}://${host}`;
}

interface BatchPresignRequest {
  file_ids: string[];
  expires_in?: number;
  disposition?: "inline" | "attachment";
}

interface BatchPresignResult {
  file_id: string;
  url?: string;
  expires_at?: string;
  expires_in?: number;
  disposition?: FileUrlDisposition;
  error?: string;
}

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;
  const record = getDatabase(name);
  if (!record) return NextResponse.json({ error: "Database not found" }, { status: 404 });

  const authError = authorizeDbRequest(req, record);
  if (authError) return authError;

  let body: BatchPresignRequest;
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "Invalid JSON body" }, { status: 400 });
  }

  if (!Array.isArray(body.file_ids) || body.file_ids.length === 0) {
    return NextResponse.json({ error: "file_ids must be a non-empty array" }, { status: 400 });
  }

  if (body.file_ids.length > 100) {
    return NextResponse.json({ error: "Maximum 100 file IDs per request" }, { status: 400 });
  }

  const expiresIn = Number.isFinite(body.expires_in) ? Number(body.expires_in) : undefined;
  const disposition = body.disposition === "attachment" ? "attachment" : "inline";
  const origin = normalizeOrigin(req);

  const results: BatchPresignResult[] = [];

  for (const fileId of body.file_ids) {
    if (typeof fileId !== "string" || !fileId) {
      results.push({ file_id: fileId, error: "Invalid file ID" });
      continue;
    }

    const file = getFileById(name, fileId);
    if (!file) {
      results.push({ file_id: fileId, error: "File not found" });
      continue;
    }

    if (isFileExpired(file)) {
      results.push({ file_id: fileId, error: "File expired" });
      continue;
    }

    try {
      const result = createPresignedFileUrl({
        origin,
        dbName: name,
        fileId,
        expiresInSeconds: expiresIn,
        disposition: disposition as FileUrlDisposition,
      });

      results.push({
        file_id: fileId,
        url: result.url,
        expires_at: result.expiresAt,
        expires_in: result.expiresIn,
        disposition: result.disposition,
      });
    } catch (error) {
      results.push({
        file_id: fileId,
        error: (error as Error).message || "Failed to create presigned URL",
      });
    }
  }

  return NextResponse.json({
    results,
    token_type: "signed_query",
    total: results.length,
    successful: results.filter((r) => !r.error).length,
    failed: results.filter((r) => r.error).length,
  });
}
