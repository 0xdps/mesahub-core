import { authorizeDbRequest } from "@/lib/auth";
import { authorizeFileAccessToken } from "@/lib/file-access-token";
import { getSignedRequestDisposition, isValidPresignedFileRequest } from "@/lib/file-url-signing";
import { deleteFile, getBlobAbsolutePath, getFileById, isFileExpired } from "@/lib/file-storage";
import { getDatabase } from "@/lib/registry";
import { recordAuditEvent } from "@/lib/registry";
import fs from "fs";
import { NextResponse } from "next/server";
import { Readable } from "stream";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string; id: string }>;
}

export async function OPTIONS() {
  return new NextResponse(null, {
    status: 204,
    headers: {
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Methods": "GET, HEAD, OPTIONS",
      "Access-Control-Allow-Headers": "Authorization, Content-Type",
      "Access-Control-Max-Age": "86400",
    },
  });
}

function contentDisposition(filename: string): string {
  const safe = filename.replace(/["\\]/g, "_");
  return `inline; filename="${safe}"`;
}

function attachmentDisposition(filename: string): string {
  const safe = filename.replace(/["\\]/g, "_");
  return `attachment; filename="${safe}"`;
}

function buildFileHeaders(
  file: NonNullable<ReturnType<typeof getFileById>>,
  disposition: "inline" | "attachment" = "inline"
): HeadersInit {
  return {
    "Content-Type": file.content_type ?? "application/octet-stream",
    "Content-Length": String(file.size_bytes),
    "Content-Disposition":
      disposition === "attachment"
        ? attachmentDisposition(file.filename)
        : contentDisposition(file.filename),
    "X-Content-Hash": file.content_hash,
    ETag: `"${file.content_hash}"`,
    "Cache-Control": "public, max-age=31536000, immutable",
    "Access-Control-Allow-Origin": "*",
    "Access-Control-Allow-Methods": "GET, HEAD, OPTIONS",
    "Access-Control-Allow-Headers": "Authorization, Content-Type",
  };
}

export async function HEAD(req: Request, { params }: Params) {
  const { name, id } = await params;
  const record = getDatabase(name);
  if (!record) return new NextResponse(null, { status: 404 });

  const hasSignedAccess = isValidPresignedFileRequest(req, name, id);
  const hasTokenAccess = !hasSignedAccess && authorizeFileAccessToken(req, name);
  
  if (!hasSignedAccess && !hasTokenAccess) {
    const authError = authorizeDbRequest(req, record);
    if (authError) return authError;
  }

  const file = getFileById(name, id);
  if (!file) return new NextResponse(null, { status: 404 });
  if (isFileExpired(file)) return new NextResponse(null, { status: 410 });

  const disposition = hasSignedAccess ? getSignedRequestDisposition(req) : "inline";
  return new NextResponse(null, { headers: buildFileHeaders(file, disposition) });
}

export async function GET(req: Request, { params }: Params) {
  const { name, id } = await params;
  const record = getDatabase(name);
  if (!record) return NextResponse.json({ error: "Database not found" }, { status: 404 });

  const hasSignedAccess = isValidPresignedFileRequest(req, name, id);
  const hasTokenAccess = !hasSignedAccess && authorizeFileAccessToken(req, name);

  if (!hasSignedAccess && !hasTokenAccess) {
    const authError = authorizeDbRequest(req, record);
    if (authError) return authError;
  }

  const file = getFileById(name, id);
  if (!file) return NextResponse.json({ error: "File not found" }, { status: 404 });
  if (isFileExpired(file)) return NextResponse.json({ error: "File expired" }, { status: 410 });

  const disposition = hasSignedAccess ? getSignedRequestDisposition(req) : "inline";
  const proxyEnabled = (process.env.ENABLE_FILE_PROXY_DELIVERY ?? "true").toLowerCase() !== "false";

  if (proxyEnabled) {
    // Do NOT include Content-Length — body is null (X-Sendfile pattern), so the file size
    // would lie to Caddy and stall it waiting for bytes. Caddy's file_server sets its own.
    const headersWithoutLength = buildFileHeaders(file, disposition) as Record<string, string>;
    delete headersWithoutLength["Content-Length"];
    return new NextResponse(null, {
      headers: { ...headersWithoutLength, "X-Sendfile": "/" + file.content_hash },
    });
  }

  const blobPath = getBlobAbsolutePath(file.content_hash);
  const stream = fs.createReadStream(blobPath);
  return new NextResponse(Readable.toWeb(stream) as ReadableStream, {
    headers: buildFileHeaders(file, disposition),
  });
}

export async function DELETE(req: Request, { params }: Params) {
  const { name, id } = await params;
  const record = getDatabase(name);
  if (!record) return NextResponse.json({ error: "Database not found" }, { status: 404 });

  const tokenPayload = authorizeFileAccessToken(req, name);
  if (tokenPayload) {
    return NextResponse.json(
      { error: "File access tokens are read-only and cannot delete files" },
      { status: 403 }
    );
  } else {
    const authError = authorizeDbRequest(req, record);
    if (authError) return authError;
  }

  if (record.status !== "active") {
    return NextResponse.json({ error: "Database is inactive - writes are not allowed" }, { status: 403 });
  }

  const deleted = deleteFile(name, id);
  if (!deleted) {
    return NextResponse.json({ error: "File not found" }, { status: 404 });
  }

  recordAuditEvent({
    eventType: "file.delete",
    dbName: name,
    actor: req.headers.get("x-sqlite-hub-admin") === "1" ? "admin_session" : "service_secret",
    metadata: { file_id: id },
  });

  return new NextResponse(null, { status: 204 });
}
