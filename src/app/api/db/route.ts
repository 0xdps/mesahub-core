import { getDbPath, getFileSizeBytes, getVolumeUsagePercent } from "@/lib/fs";
import { logger } from "@/lib/logger";
import {
  DbRecord,
  getRegistry,
  insertDatabase,
  listDatabases,
} from "@/lib/registry";
import Database from "better-sqlite3";
import { randomBytes } from "crypto";
import fs from "fs";
import { NextResponse } from "next/server";
import path from "path";

const NAME_REGEX = /^[a-z0-9_-]+$/;
const MAX_USAGE = parseInt(process.env.MAX_VOLUME_USAGE_PERCENT ?? "85", 10);
const DATA_PATH = process.env.DATA_PATH ?? "/data";
const ADMIN_SESSION_HEADER = "x-sqlite-hub-admin";

function withStats(record: DbRecord) {
  const filePath = getDbPath(record.name);
  return {
    ...record,
    file_path: filePath,
    size_bytes: getFileSizeBytes(filePath),
    exists: fs.existsSync(filePath),
  };
}

export async function GET(req: Request) {
  if (req.headers.get(ADMIN_SESSION_HEADER) !== "1") {
    return NextResponse.json({ error: "Admin session required" }, { status: 401 });
  }
  const rows = listDatabases().map(withStats);
  return NextResponse.json(rows);
}

export async function POST(req: Request) {
  if (req.headers.get(ADMIN_SESSION_HEADER) !== "1") {
    return NextResponse.json({ error: "Admin session required" }, { status: 401 });
  }

  const body = await req.json().catch(() => null);

  if (!body || typeof body.name !== "string" || typeof body.owner !== "string") {
    logger.warn("[db] Create failed — missing name or owner");
    return NextResponse.json({ error: "name and owner are required" }, { status: 400 });
  }

  const { name, owner, description, generate_secret } = body as {
    name: string;
    owner: string;
    description?: string;
    generate_secret?: boolean;
  };

  if (!NAME_REGEX.test(name)) {
    logger.warn(`[db] Create failed — invalid name: "${name}"`);
    return NextResponse.json(
      { error: "name must match ^[a-z0-9_-]+$" },
      { status: 400 }
    );
  }

  const diskUsage = getVolumeUsagePercent();
  if (diskUsage >= MAX_USAGE) {
    logger.warn(`[db] Create "${name}" blocked — volume usage ${diskUsage}% >= limit ${MAX_USAGE}%`);
    return NextResponse.json(
      { error: `Volume usage ${diskUsage}% exceeds limit of ${MAX_USAGE}%` },
      { status: 507 }
    );
  }

  const existing = getRegistry()
    .prepare("SELECT id, status FROM databases WHERE name = ?")
    .get(name) as { id: number; status: string } | undefined;

  if (existing) {
    logger.warn(`[db] Create failed — database "${name}" already exists`);
    return NextResponse.json({ error: "Database already exists" }, { status: 409 });
  }

  // Create the SQLite file with WAL mode
  const dbPath = path.join(DATA_PATH, `${name}.db`);
  const newDb = new Database(dbPath);
  newDb.pragma("journal_mode = WAL");
  newDb.close();

  const serviceSecret = generate_secret
    ? `shs_${randomBytes(32).toString("hex")}`
    : undefined;

  const record = insertDatabase(name, owner, description, serviceSecret);
  logger.info(`[db] Created database "${name}" (owner: "${owner}"${serviceSecret ? ", with service secret" : ""})`);


  return NextResponse.json(
    {
      ...withStats(record),
      // Return the plaintext secret only at creation time — never surfaced again
      ...(serviceSecret ? { service_secret: serviceSecret } : {}),
    },
    { status: 201 }
  );
}
