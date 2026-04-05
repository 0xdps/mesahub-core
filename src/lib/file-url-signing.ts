import { createHmac, timingSafeEqual } from "crypto";

export type FileUrlDisposition = "inline" | "attachment";

interface SignInput {
  dbName: string;
  fileId: string;
  expiresAtEpochSeconds: number;
  disposition: FileUrlDisposition;
}

const DEFAULT_PRESIGN_TTL_SECONDS = Number.parseInt(
  process.env.FILE_PRESIGN_DEFAULT_TTL_SECONDS ?? "900",
  10
);
const MAX_PRESIGN_TTL_SECONDS = Number.parseInt(
  process.env.FILE_PRESIGN_MAX_TTL_SECONDS ?? "86400",
  10
);

function getSigningSecret(): string {
  const secret = process.env.FILE_URL_SIGNING_SECRET || process.env.ADMIN_TOKEN || "";

  if (!secret) {
    throw new Error("Missing FILE_URL_SIGNING_SECRET (or ADMIN_TOKEN as fallback) for file URL signing");
  }

  return secret;
}

function clampTtlSeconds(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return DEFAULT_PRESIGN_TTL_SECONDS;
  if (value > MAX_PRESIGN_TTL_SECONDS) return MAX_PRESIGN_TTL_SECONDS;
  return Math.floor(value);
}

function toSignPayload(input: SignInput): string {
  return [
    "GET",
    input.dbName,
    input.fileId,
    String(input.expiresAtEpochSeconds),
    input.disposition,
  ].join("\n");
}

function signPayload(payload: string): string {
  return createHmac("sha256", getSigningSecret()).update(payload).digest("hex");
}

function normalizeDisposition(value: string | null | undefined): FileUrlDisposition {
  return value === "attachment" ? "attachment" : "inline";
}

export function resolvePresignTtlSeconds(expiresInSeconds?: number): number {
  const requested = expiresInSeconds ?? DEFAULT_PRESIGN_TTL_SECONDS;
  return clampTtlSeconds(requested);
}

export function createSignedFileQuery(input: SignInput): URLSearchParams {
  const params = new URLSearchParams();
  params.set("exp", String(input.expiresAtEpochSeconds));
  if (input.disposition === "attachment") {
    params.set("dl", "1");
  }

  const sig = signPayload(toSignPayload(input));
  params.set("sig", sig);
  return params;
}

export function createPresignedFileUrl(input: {
  origin: string;
  dbName: string;
  fileId: string;
  expiresInSeconds?: number;
  disposition?: FileUrlDisposition;
}): { url: string; expiresAt: string; expiresIn: number; disposition: FileUrlDisposition } {
  const expiresIn = resolvePresignTtlSeconds(input.expiresInSeconds);
  const expiresAtEpochSeconds = Math.floor(Date.now() / 1000) + expiresIn;
  const disposition = input.disposition ?? "inline";
  const query = createSignedFileQuery({
    dbName: input.dbName,
    fileId: input.fileId,
    expiresAtEpochSeconds,
    disposition,
  });

  const path = `/api/db/${encodeURIComponent(input.dbName)}/files/${encodeURIComponent(input.fileId)}`;
  const url = `${input.origin}${path}?${query.toString()}`;

  return {
    url,
    expiresAt: new Date(expiresAtEpochSeconds * 1000).toISOString(),
    expiresIn,
    disposition,
  };
}

export function isValidPresignedFileRequest(req: Request, dbName: string, fileId: string): boolean {
  const url = new URL(req.url);
  const expRaw = url.searchParams.get("exp");
  const sig = url.searchParams.get("sig");
  if (!expRaw || !sig) return false;

  const exp = Number.parseInt(expRaw, 10);
  if (!Number.isFinite(exp) || exp <= 0) return false;
  if (Math.floor(Date.now() / 1000) > exp) return false;

  const disposition = normalizeDisposition(url.searchParams.get("dl") === "1" ? "attachment" : "inline");
  const payload = toSignPayload({
    dbName,
    fileId,
    expiresAtEpochSeconds: exp,
    disposition,
  });

  const expected = signPayload(payload);
  const a = Buffer.from(sig, "utf8");
  const b = Buffer.from(expected, "utf8");
  return a.length === b.length && timingSafeEqual(a, b);
}

export function getSignedRequestDisposition(req: Request): FileUrlDisposition {
  const url = new URL(req.url);
  return url.searchParams.get("dl") === "1" ? "attachment" : "inline";
}