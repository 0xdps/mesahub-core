import { createHash, timingSafeEqual } from "crypto";
import fs from "fs";
import path from "path";
import BetterSqlite3 from "better-sqlite3";
import { NextResponse } from "next/server";
import type { DbRecord } from "./registry";

// Must match the constant exported from middleware
const ADMIN_SESSION_HEADER = "x-sqlite-hub-admin";
const ALLOW_INTERNAL_DB_ACCESS_WITHOUT_SECRET =
  (process.env.ALLOW_INTERNAL_DB_ACCESS_WITHOUT_SECRET ?? "false").toLowerCase() === "true";

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

function isPrivateIP(ip: string): boolean {
  // Strip IPv6-mapped IPv4 prefix (::ffff:x.x.x.x)
  const addr = ip.replace(/^::ffff:/, "").trim();
  if (addr === "127.0.0.1" || addr === "::1" || addr === "localhost") return true;

  // Railway private network uses fd12::/7 IPv6 ULA addresses
  if (/^fd[0-9a-f]{2}:/i.test(addr)) return true;

  // RFC1918 IPv4 private ranges
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
 *  2. DB has service_secret, bearer matches → allowed (scoped to this DB)
 *  3. DB has service_secret, wrong/no bearer → 401 Unauthorized
 *  4. DB has no service_secret, internal IP  → allowed
 *  5. DB has no service_secret, public IP    → 403 Forbidden
 *
 * NOTE: ADMIN_TOKEN is intentionally NOT accepted here. It is only valid at
 * the /api/auth/login endpoint to obtain a session cookie. All programmatic
 * access must use a per-DB service_secret or a user-scoped shs_ API key.
 */

/**
 * Validates an `shs_` API key against the control database.
 * Returns the owning user_id if valid, null otherwise.
 * Also updates last_used_at on successful validation.
 */
function validateApiKey(keyValue: string): string | null {
  try {
    const DATA_PATH = process.env.DATA_PATH ?? "/data";
    const controlDbPath = path.join(DATA_PATH, "control.db");
    if (!fs.existsSync(controlDbPath)) return null;

    const db = new BetterSqlite3(controlDbPath);
    const keyHash = createHash("sha256").update(keyValue).digest("hex");

    const row = db
      .prepare("SELECT id, user_id FROM api_keys WHERE key_hash = ? AND status = 'active'")
      .get(keyHash) as { id: string; user_id: string } | undefined;

    if (!row) {
      db.close();
      return null;
    }

    // Stamp last_used_at
    db.prepare("UPDATE api_keys SET last_used_at = datetime('now') WHERE id = ?").run(row.id);
    const userId = row.user_id;
    db.close();
    return userId;
  } catch {
    return null;
  }
}

export function authorizeDbRequest(req: Request, record: DbRecord): NextResponse | null {
  // Rule 1: admin browser session (header stamped by middleware, forgery-stripped)
  if (req.headers.get(ADMIN_SESSION_HEADER) === "1") return null;

  // Inactive DBs: reject all non-admin access
  if (record.status !== "active") {
    return NextResponse.json({ error: "This database is inactive" }, { status: 503 });
  }

  const bearer = extractBearer(req);

  // Rule: shs_ API keys — user-scoped, valid for any DB owned by that user
  if (bearer?.startsWith("shs_")) {
    const userId = validateApiKey(bearer);
    if (userId && record.owner === userId) return null;
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  if (record.service_secret) {
    // Rule 2 & 3: per-DB service secret
    if (bearer && timingSafeMatch(bearer, record.service_secret)) return null;
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  // No service_secret: internal bypass is opt-in only.
  if (ALLOW_INTERNAL_DB_ACCESS_WITHOUT_SECRET && isInternalRequest(req)) return null;
  return NextResponse.json(
    { error: "Forbidden: this database has no service_secret — set a service_secret for API access" },
    { status: 403 }
  );
}
