import { authorizeDbRequest } from "@/lib/auth";
import { authorizeFileAccessToken } from "@/lib/file-access-token";
import { getSignedRequestDisposition, isValidPresignedFileRequest } from "@/lib/file-url-signing";
import { deleteFile, getBlobAbsolutePath, getFileById, isFileExpired } from "@/lib/file-storage";
import { getDatabase } from "@/lib/registry";
import fs from "fs";
import { NextResponse } from "next/server";
import { Readable } from "stream";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string; id: string }>;
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
    return new NextResponse(null, {
      headers: {
        ...buildFileHeaders(file, disposition),
        "X-Sendfile": file.content_hash,
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

  return new NextResponse(null, { status: 204 });
}
