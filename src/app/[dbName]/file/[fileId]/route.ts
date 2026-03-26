import { authorizeFileAccessToken } from "@/lib/file-access-token";
import { getSignedRequestDisposition, isValidPresignedFileRequest } from "@/lib/file-url-signing";
import { getBlobAbsolutePath, getFileById, isFileExpired } from "@/lib/file-storage";
import fs from "fs";
import { NextResponse } from "next/server";
import { Readable } from "stream";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ dbName: string; fileId: string }>;
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
  };
}

/**
 * Public file access route with CORS support.
 * Supports presigned URLs and file access tokens only (no admin session).
 * Path: /{dbName}/file/{fileId}
 */
export async function GET(req: Request, { params }: Params) {
  const { dbName, fileId } = await params;

  const hasSignedAccess = isValidPresignedFileRequest(req, dbName, fileId);
  const hasTokenAccess = !hasSignedAccess && authorizeFileAccessToken(req, dbName);

  if (!hasSignedAccess && !hasTokenAccess) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const file = getFileById(dbName, fileId);
  if (!file) return NextResponse.json({ error: "File not found" }, { status: 404 });
  if (isFileExpired(file)) return NextResponse.json({ error: "File expired" }, { status: 410 });

  const disposition = hasSignedAccess ? getSignedRequestDisposition(req) : "inline";

  const proxyEnabled = (process.env.ENABLE_FILE_PROXY_DELIVERY ?? "true").toLowerCase() !== "false";

  if (proxyEnabled) {
    // Do NOT include Content-Length — body is null/empty, so the file size would
    // lie to Caddy and cause it to stall waiting for bytes. file_server sets its own.
    const { "Content-Length": _drop, ...headersWithoutLength } = buildFileHeaders(file, disposition) as Record<string, string>;
    return new NextResponse(null, {
      headers: {
        ...headersWithoutLength,
        "X-Sendfile": "/" + file.content_hash,
      },
    });
  }

  const blobPath = getBlobAbsolutePath(file.content_hash);
  if (!fs.existsSync(blobPath)) {
    return NextResponse.json({ error: "Blob not found" }, { status: 404 });
  }

  const stream = fs.createReadStream(blobPath);
  return new NextResponse(Readable.toWeb(stream) as ReadableStream, {
    headers: buildFileHeaders(file, disposition),
  });
}

export async function HEAD(req: Request, { params }: Params) {
  const { dbName, fileId } = await params;

  const hasSignedAccess = isValidPresignedFileRequest(req, dbName, fileId);
  const hasTokenAccess = !hasSignedAccess && authorizeFileAccessToken(req, dbName);

  if (!hasSignedAccess && !hasTokenAccess) {
    return new NextResponse(null, { status: 401 });
  }

  const file = getFileById(dbName, fileId);
  if (!file) return new NextResponse(null, { status: 404 });
  if (isFileExpired(file)) return new NextResponse(null, { status: 410 });

  const disposition = hasSignedAccess ? getSignedRequestDisposition(req) : "inline";
  return new NextResponse(null, { headers: buildFileHeaders(file, disposition) });
}
