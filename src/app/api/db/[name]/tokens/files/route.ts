import { authorizeDbRequest } from "@/lib/auth";
import { createFileAccessToken, type CreateFileAccessTokenOptions } from "@/lib/file-access-token";
import { getDatabase } from "@/lib/registry";
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

  const scope = "files:read";
  const expiresIn = Number.isFinite(body.expires_in) ? Number(body.expires_in) : undefined;

  try {
    const options: CreateFileAccessTokenOptions = {
      dbName: name,
      scope,
      expiresInSeconds: expiresIn,
    };

    const result = createFileAccessToken(options);

    return NextResponse.json({
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
