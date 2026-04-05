import { authorizeDbRequest } from "@/lib/auth";
import { createPresignedFileUrl, type FileUrlDisposition } from "@/lib/file-url-signing";
import { getFileById, isFileExpired } from "@/lib/file-storage";
import { getDatabase } from "@/lib/registry";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string; id: string }>;
}

function normalizeOrigin(req: Request): string {
  const url = new URL(req.url);
  const proto = req.headers.get("x-forwarded-proto") ?? url.protocol.replace(":", "");
  const host = req.headers.get("x-forwarded-host") ?? req.headers.get("host") ?? url.host;
  return `${proto}://${host}`;
}

export async function POST(req: Request, { params }: Params) {
  const { name, id } = await params;
  const record = getDatabase(name);
  if (!record) return NextResponse.json({ error: "Database not found" }, { status: 404 });

  const authError = authorizeDbRequest(req, record);
  if (authError) return authError;

  const file = getFileById(name, id);
  if (!file) return NextResponse.json({ error: "File not found" }, { status: 404 });
  if (isFileExpired(file)) return NextResponse.json({ error: "File expired" }, { status: 410 });

  const body = await req.json().catch(() => ({}));
  const expiresInRaw = body?.expires_in;
  const expiresIn = Number.isFinite(expiresInRaw) ? Number(expiresInRaw) : undefined;
  const disposition = body?.disposition === "attachment" ? "attachment" : "inline";

  try {
    const origin = normalizeOrigin(req);
    const result = createPresignedFileUrl({
      origin,
      dbName: name,
      fileId: id,
      expiresInSeconds: expiresIn,
      disposition: disposition as FileUrlDisposition,
    });

    return NextResponse.json({
      url: result.url,
      expires_at: result.expiresAt,
      expires_in: result.expiresIn,
      disposition: result.disposition,
      token_type: "signed_query",
    });
  } catch (error) {
    return NextResponse.json(
      { error: (error as Error).message || "Failed to create presigned URL" },
      { status: 500 }
    );
  }
}