import { authorizeDbRequest } from "@/lib/auth";
import { verifyFileAccessToken } from "@/lib/file-access-token";
import { getDatabase, recordAuditEvent, revokeFileToken } from "@/lib/registry";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string }>;
}

interface RevokeFileTokenRequest {
  token?: string;
  token_id?: string;
  expires_at?: string;
  reason?: string;
}

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;
  const record = getDatabase(name);
  if (!record) return NextResponse.json({ error: "Database not found" }, { status: 404 });

  const authError = authorizeDbRequest(req, record);
  if (authError) return authError;

  let body: RevokeFileTokenRequest;
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "Invalid JSON body" }, { status: 400 });
  }

  let tokenId: string | undefined;
  let expiresAt: string | undefined;

  if (body.token) {
    const payload = verifyFileAccessToken(body.token);
    if (!payload || payload.dbName !== name) {
      return NextResponse.json({ error: "Invalid token" }, { status: 400 });
    }
    tokenId = payload.tokenId;
    expiresAt = new Date(payload.expiresAt * 1000).toISOString();
  } else {
    if (!body.token_id || typeof body.token_id !== "string") {
      return NextResponse.json(
        { error: "Provide token or token_id" },
        { status: 400 }
      );
    }
    if (!body.expires_at || typeof body.expires_at !== "string") {
      return NextResponse.json(
        { error: "expires_at is required when revoking by token_id" },
        { status: 400 }
      );
    }
    tokenId = body.token_id;
    expiresAt = body.expires_at;
  }

  revokeFileToken({
    tokenId,
    dbName: name,
    expiresAt,
    reason: body.reason,
  });

  recordAuditEvent({
    eventType: "file_token.revoked",
    dbName: name,
    actor: req.headers.get("x-sqlite-hub-admin") === "1" ? "admin_session" : "service_secret",
    metadata: {
      token_id: tokenId,
      reason: body.reason || null,
      expires_at: expiresAt,
    },
  });

  return NextResponse.json({
    success: true,
    token_id: tokenId,
    revoked_at: new Date().toISOString(),
  });
}
