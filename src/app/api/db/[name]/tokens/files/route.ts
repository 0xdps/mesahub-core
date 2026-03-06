import { authorizeDbRequest } from "@/lib/auth";
import { createFileAccessToken, type CreateFileAccessTokenOptions } from "@/lib/file-access-token";
import { getDatabase } from "@/lib/registry";
import { recordAuditEvent } from "@/lib/registry";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string }>;
}

interface CreateTokenRequest {
  scope?: "files:read";
  expires_in?: number;
  description?: string;
}

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;
  const record = getDatabase(name);
  if (!record) return NextResponse.json({ error: "Database not found" }, { status: 404 });

  const authError = authorizeDbRequest(req, record);
  if (authError) return authError;

  let body: CreateTokenRequest;
  try {
    body = await req.json().catch(() => ({}));
  } catch {
    return NextResponse.json({ error: "Invalid JSON body" }, { status: 400 });
  }

  if (body.scope && body.scope !== "files:read") {
    return NextResponse.json(
      { error: "scope must be 'files:read'" },
      { status: 400 }
    );
  }

  const scope = "files:read";
  const expiresIn = Number.isFinite(body.expires_in) ? Number(body.expires_in) : undefined;

  try {
    const options: CreateFileAccessTokenOptions = {
      dbName: name,
      scope,
      expiresInSeconds: expiresIn,
    };

    const result = createFileAccessToken(options);

    recordAuditEvent({
      eventType: "file_token.created",
      dbName: name,
      actor: req.headers.get("x-sqlite-hub-admin") === "1" ? "admin_session" : "service_secret",
      metadata: {
        scope: result.scope,
        expires_at: result.expires_at,
        description: body.description || null,
      },
    });

    return NextResponse.json({
      token_id: result.token_id,
      token: result.token,
      token_type: "bearer",
      expires_at: result.expires_at,
      expires_in: result.expires_in,
      scope: result.scope,
      description: body.description || undefined,
      usage:
        'Use as query parameter: ?token=... or Authorization header: "Bearer <token>"',
    });
  } catch (error) {
    return NextResponse.json(
      { error: (error as Error).message || "Failed to create access token" },
      { status: 500 }
    );
  }
}
