import { getDbPath, getFileSizeBytes } from "@/lib/fs";
import { listDeletedDatabases, type DbRecord } from "@/lib/registry";
import fs from "fs";
import { NextResponse } from "next/server";

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
  const rows = listDeletedDatabases().map(withStats);
  return NextResponse.json(rows);
}
