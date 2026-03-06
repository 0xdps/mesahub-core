import { createHmac, randomUUID, timingSafeEqual } from "crypto";
import { isFileTokenRevoked } from "./registry";

const DEFAULT_TOKEN_TTL_SECONDS = 60 * 60 * 24 * 30; // 30 days
const MAX_TOKEN_TTL_SECONDS = 60 * 60 * 24 * 365; // 1 year

function getSigningSecret(): string {
  const secret = process.env.FILE_TOKEN_SIGNING_SECRET;

  if (!secret) {
    throw new Error("Missing FILE_TOKEN_SIGNING_SECRET");
  }

  return secret;
}

export interface FileAccessTokenPayload {
  tokenId: string;
  dbName: string;
  scope: "files:read";
  expiresAt: number;
}

export interface CreateFileAccessTokenOptions {
  dbName: string;
  scope?: "files:read";
  expiresInSeconds?: number;
}

export interface CreateFileAccessTokenResult {
  token_id: string;
  token: string;
  expires_at: string;
  expires_in: number;
  scope: string;
}

/**
 * Creates a long-lived access token for file operations.
 * Token format: base64(payload).signature
 */
export function createFileAccessToken(
  options: CreateFileAccessTokenOptions
): CreateFileAccessTokenResult {
  const expiresIn = Math.min(
    Math.max(options.expiresInSeconds ?? DEFAULT_TOKEN_TTL_SECONDS, 60),
    MAX_TOKEN_TTL_SECONDS
  );

  const expiresAt = Math.floor(Date.now() / 1000) + expiresIn;
  const scope = options.scope ?? "files:read";

  const payload: FileAccessTokenPayload = {
    tokenId: randomUUID(),
    dbName: options.dbName,
    scope,
    expiresAt,
  };

  const payloadStr = JSON.stringify(payload);
  const payloadB64 = Buffer.from(payloadStr, "utf-8").toString("base64url");

  const secret = getSigningSecret();
  const signature = createHmac("sha256", secret).update(payloadB64).digest("base64url");

  const token = `${payloadB64}.${signature}`;

  return {
    token_id: payload.tokenId,
    token,
    expires_at: new Date(expiresAt * 1000).toISOString(),
    expires_in: expiresIn,
    scope,
  };
}

/**
 * Verifies and decodes a file access token.
 * Returns null if invalid or expired.
 */
export function verifyFileAccessToken(token: string): FileAccessTokenPayload | null {
  if (!token || typeof token !== "string") return null;

  const parts = token.split(".");
  if (parts.length !== 2) return null;

  const [payloadB64, providedSig] = parts;

  try {
    const secret = getSigningSecret();
    const expectedSig = createHmac("sha256", secret).update(payloadB64).digest("base64url");

    const providedSigBuf = Buffer.from(providedSig, "base64url");
    const expectedSigBuf = Buffer.from(expectedSig, "base64url");

    if (
      providedSigBuf.length !== expectedSigBuf.length ||
      !timingSafeEqual(providedSigBuf, expectedSigBuf)
    ) {
      return null;
    }

    const payloadStr = Buffer.from(payloadB64, "base64url").toString("utf-8");
    const payload: FileAccessTokenPayload = JSON.parse(payloadStr);

    if (typeof payload.dbName !== "string" || !payload.dbName) return null;
    if (typeof payload.tokenId !== "string" || !payload.tokenId) return null;
       if (payload.scope !== "files:read") return null;
    if (typeof payload.expiresAt !== "number") return null;

    const now = Math.floor(Date.now() / 1000);
    if (payload.expiresAt < now) return null;

    return payload;
  } catch {
    return null;
  }
}

/**
 * Checks if the request contains a valid file access token.
 * Supports both query parameter (?token=...) and Authorization header.
 */
export function getFileAccessTokenFromRequest(req: Request): string | null {
  // Check query parameter first
  const url = new URL(req.url);
  const queryToken = url.searchParams.get("token");
  if (queryToken) return queryToken;

  // Check Authorization header
  const authHeader = req.headers.get("authorization");
  if (authHeader?.startsWith("Bearer ")) {
    return authHeader.slice(7);
  }

  return null;
}

/**
 * Validates file access token from request and returns payload if valid.
 */
export function authorizeFileAccessToken(
  req: Request,
  dbName: string
): FileAccessTokenPayload | null {
  const token = getFileAccessTokenFromRequest(req);
  if (!token) return null;

  const payload = verifyFileAccessToken(token);
  if (!payload) return null;

  if (payload.dbName !== dbName) return null;
  if (isFileTokenRevoked(payload.tokenId, dbName)) return null;

  return payload;
}
