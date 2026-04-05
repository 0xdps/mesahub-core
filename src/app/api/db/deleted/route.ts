import { getDbPath, getFileSizeBytes } from "@/lib/fs";
import { listDeletedDatabases, type DbRecord } from "@/lib/registry";
import fs from "fs";
import { NextResponse } from "next/server";

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
  const rows = listDeletedDatabases().map(withStats);
  return NextResponse.json(rows);
}
