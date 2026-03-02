import { getDbPath, getFileSizeBytes, getVolumeUsagePercent } from "@/lib/fs";
import {
  DbRecord,
  getRegistry,
  insertDatabase,
  listDatabases,
} from "@/lib/registry";
import Database from "better-sqlite3";
import fs from "fs";
import { NextResponse } from "next/server";
import path from "path";

const NAME_REGEX = /^[a-z0-9_-]+$/;
const MAX_USAGE = parseInt(process.env.MAX_VOLUME_USAGE_PERCENT ?? "85", 10);
const DATA_PATH = process.env.DATA_PATH ?? "/data";

function withStats(record: DbRecord) {
  const filePath = getDbPath(record.name);
  return {
    ...record,
    file_path: filePath,
    size_bytes: getFileSizeBytes(filePath),
    exists: fs.existsSync(filePath),
  };
}

export async function GET() {
  const rows = listDatabases().map(withStats);
  return NextResponse.json(rows);
}

export async function POST(req: Request) {
  const body = await req.json().catch(() => null);

  if (!body || typeof body.name !== "string" || typeof body.owner !== "string") {
    return NextResponse.json({ error: "name and owner are required" }, { status: 400 });
  }

  const { name, owner, description } = body as {
    name: string;
    owner: string;
    description?: string;
  };

  if (!NAME_REGEX.test(name)) {
    return NextResponse.json(
      { error: "name must match ^[a-z0-9_-]+$" },
      { status: 400 }
    );
  }

  const diskUsage = getVolumeUsagePercent();
  if (diskUsage >= MAX_USAGE) {
    return NextResponse.json(
      { error: `Volume usage ${diskUsage}% exceeds limit of ${MAX_USAGE}%` },
      { status: 507 }
    );
  }

  const existing = getRegistry()
    .prepare("SELECT id FROM databases WHERE name = ?")
    .get(name);
  if (existing) {
    return NextResponse.json({ error: "Database already exists" }, { status: 409 });
  }

  // Create the SQLite file with WAL mode
  const dbPath = path.join(DATA_PATH, `${name}.db`);
  const newDb = new Database(dbPath);
  newDb.pragma("journal_mode = WAL");
  newDb.close();

  const record = insertDatabase(name, owner, description);
  return NextResponse.json(withStats(record), { status: 201 });
}
