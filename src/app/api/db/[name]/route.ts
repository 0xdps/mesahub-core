import { getDbPath, getFileSizeBytes } from "@/lib/fs";
import { logger } from "@/lib/logger";
import { getDatabase, setDatabaseStatus, softDeleteDatabase, updateServiceSecret } from "@/lib/registry";
import { randomBytes } from "crypto";
import fs from "fs";
import { NextResponse } from "next/server";

interface Params {
  params: Promise<{ name: string }>;
}

export async function GET(_req: Request, { params }: Params) {
  const { name } = await params;
  const record = getDatabase(name);
  if (!record) {
    logger.warn(`[db] GET "${name}" — not found`);
    return NextResponse.json({ error: "Not found" }, { status: 404 });
  }
  const filePath = getDbPath(name);
  const { service_secret: _secret, ...safeRecord } = record;
  return NextResponse.json({
    ...safeRecord,
    has_service_secret: !!_secret,
    file_path: filePath,
    size_bytes: getFileSizeBytes(filePath),
    exists: fs.existsSync(filePath),
  });
}

export async function PATCH(req: Request, { params }: Params) {
  const { name } = await params;
  const record = getDatabase(name);
  if (!record) {
    logger.warn(`[db] PATCH "${name}" — not found`);
    return NextResponse.json({ error: "Not found" }, { status: 404 });
  }

  const body = await req.json().catch(() => null);
  const action = body?.action;

  if (action === "generate_secret") {
    const secret = "shs_" + randomBytes(32).toString("hex");
    updateServiceSecret(name, secret);
    logger.info(`[db] Service secret generated for "${name}"`);
    return NextResponse.json({ service_secret: secret });
  }

  if (action === "revoke_secret") {
    updateServiceSecret(name, null);
    logger.info(`[db] Service secret revoked for "${name}"`);
    return NextResponse.json({ success: true });
  }

  if (action === "set_status") {
    const status = body?.status;
    if (status !== "active" && status !== "inactive") {
      logger.warn(`[db] set_status "${name}" — invalid value: "${status}"`);
      return NextResponse.json({ error: "status must be 'active' or 'inactive'" }, { status: 400 });
    }
    setDatabaseStatus(name, status);
    logger.info(`[db] Status set to "${status}" for "${name}"`);
    return NextResponse.json({ success: true, status });
  }

  logger.warn(`[db] PATCH "${name}" — invalid action: "${action}"`);
  return NextResponse.json({ error: "Invalid action" }, { status: 400 });
}

export async function DELETE(_req: Request, { params }: Params) {
  const { name } = await params;
  const record = getDatabase(name);
  if (!record) {
    logger.warn(`[db] DELETE "${name}" — not found`);
    return NextResponse.json({ error: "Not found" }, { status: 404 });
  }
  softDeleteDatabase(name);
  logger.info(`[db] Database "${name}" deleted`);
  return NextResponse.json({ success: true });
}
