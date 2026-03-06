import { authorizeDbRequest } from "@/lib/auth";
import { FileStorageError, listFiles, uploadFile } from "@/lib/file-storage";
import { logger } from "@/lib/logger";
import { getDatabase } from "@/lib/registry";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string }>;
}

export async function GET(req: Request, { params }: Params) {
  const { name } = await params;
  const record = getDatabase(name);
  if (!record) {
    return NextResponse.json({ error: "Database not found" }, { status: 404 });
  }

  const authError = authorizeDbRequest(req, record);
  if (authError) return authError;

  const url = new URL(req.url);
  const limit = Math.min(Math.max(Number.parseInt(url.searchParams.get("limit") ?? "100", 10) || 100, 1), 1000);
  const offset = Math.max(Number.parseInt(url.searchParams.get("offset") ?? "0", 10) || 0, 0);
  const sort = url.searchParams.get("sort") ?? "uploaded_at";
  const order = (url.searchParams.get("order") ?? "desc").toLowerCase() === "asc" ? "asc" : "desc";

  const result = listFiles(name, { limit, offset, sort, order });
  return NextResponse.json(result);
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

  try {
    const formData = await req.formData();
    const fileValue = formData.get("file");
    if (!(fileValue instanceof File)) {
      return NextResponse.json({ error: "file is required" }, { status: 400 });
    }

    const filename = String(formData.get("filename") ?? fileValue.name ?? "file");
    const contentTypeRaw = String(formData.get("content_type") ?? fileValue.type ?? "").trim();
    const contentType = contentTypeRaw || null;

    const metadataRaw = formData.get("metadata");
    let metadata: Record<string, unknown> | null = null;
    if (typeof metadataRaw === "string" && metadataRaw.trim()) {
      const parsed = JSON.parse(metadataRaw);
      if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
        return NextResponse.json({ error: "metadata must be a JSON object" }, { status: 400 });
      }
      metadata = parsed as Record<string, unknown>;
    }

    const expiresInRaw = String(formData.get("expires_in") ?? "").trim();
    let expiresAt: string | null = null;
    if (expiresInRaw) {
      const seconds = Number.parseInt(expiresInRaw, 10);
      if (!Number.isFinite(seconds) || seconds <= 0) {
        return NextResponse.json({ error: "expires_in must be a positive integer" }, { status: 400 });
      }
      expiresAt = new Date(Date.now() + seconds * 1000).toISOString();
    }

    const bytes = Buffer.from(await fileValue.arrayBuffer());

    const result = uploadFile({
      dbName: name,
      filename,
      contentType,
      bytes,
      expiresAt,
      metadata,
    });

    return NextResponse.json(
      {
        id: result.id,
        filename: result.filename,
        size_bytes: result.sizeBytes,
        content_type: result.contentType,
        url: `/api/db/${encodeURIComponent(name)}/files/${result.id}`,
        uploaded_at: result.uploadedAt,
        expires_at: result.expiresAt,
      },
      { status: 201 }
    );
  } catch (error) {
    if (error instanceof FileStorageError) {
      return NextResponse.json({ error: error.message }, { status: error.status });
    }

    logger.warn(`[files] "${name}" - upload failed: ${(error as Error).message}`);
    return NextResponse.json({ error: "Failed to upload file" }, { status: 400 });
  }
}
