import { timingSafeEqual } from "crypto";
import { NextResponse } from "next/server";
import type { DbRecord } from "./registry";

// Must match the constant exported from middleware
const ADMIN_SESSION_HEADER = "x-sqlite-hub-admin";

function extractBearer(req: Request): string | null {
  const header = req.headers.get("authorization");
  if (!header?.startsWith("Bearer ")) return null;
  return header.slice(7);
}

function timingSafeMatch(a: string, b: string): boolean {
  if (!a || !b) return false;
  const bufA = Buffer.from(a.padEnd(b.length));
  const bufB = Buffer.from(b.padEnd(a.length));
  return bufA.length === bufB.length && timingSafeEqual(bufA, bufB);
}

export function isAdminToken(req: Request): boolean {
  const token = extractBearer(req);
  if (!token) return false;
  const adminToken = process.env.ADMIN_TOKEN ?? "";
  return !!adminToken && timingSafeMatch(token, adminToken);
}

function isPrivateIP(ip: string): boolean {
  // Strip IPv6-mapped IPv4 prefix (::ffff:x.x.x.x)
  const addr = ip.replace(/^::ffff:/, "").trim();
  if (addr === "127.0.0.1" || addr === "::1" || addr === "localhost") return true;
  const parts = addr.split(".").map(Number);
  if (parts.length !== 4 || parts.some(isNaN)) return false;
  const [a, b] = parts;
  return (
    a === 10 ||
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && b === 168)
  );
}

export function isInternalRequest(req: Request): boolean {
  const forwarded = req.headers.get("x-forwarded-for");
  const realIp = req.headers.get("x-real-ip");
  // x-forwarded-for may be a comma-separated list; the leftmost is the original client
  const clientIp = (forwarded?.split(",")[0] ?? realIp ?? "").trim();
  if (!clientIp) return false;
  return isPrivateIP(clientIp);
}

/**
 * Authorizes a request for a specific DB.
 * Returns null if access is granted, or a NextResponse with 401/403 if denied.
 *
 * Rules:
 *  1. Trusted admin-session header (set by middleware after cookie verification) → allowed
 *  2. ADMIN_TOKEN bearer  → always allowed
 *  3. DB has service_secret, bearer matches → allowed (scoped to this DB)
 *  4. DB has service_secret, wrong/no bearer  → 401 Unauthorized
 *  5. DB has no service_secret, internal IP   → allowed
 *  6. DB has no service_secret, public IP     → 403 Forbidden
 */
export function authorizeDbRequest(req: Request, record: DbRecord): NextResponse | null {
  // Rule 1: admin browser session (header stamped by middleware, forgery-stripped)
  if (req.headers.get(ADMIN_SESSION_HEADER) === "1") return null;

  // Rule 2: ADMIN_TOKEN always wins
  if (isAdminToken(req)) return null;

  const bearer = extractBearer(req);

  if (record.service_secret) {
    // Rule 2 & 3
    if (bearer && timingSafeMatch(bearer, record.service_secret)) return null;
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  // No service_secret: rules 4 & 5
  if (isInternalRequest(req)) return null;
  return NextResponse.json(
    { error: "Forbidden: this database requires an internal network request or an ADMIN_TOKEN" },
    { status: 403 }
  );
}
