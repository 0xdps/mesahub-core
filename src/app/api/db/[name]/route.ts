import { getDbPath, getFileSizeBytes } from "@/lib/fs";
import { getDatabase, softDeleteDatabase } from "@/lib/registry";
import fs from "fs";
import { NextResponse } from "next/server";

interface Params {
  params: Promise<{ name: string }>;
}

export async function GET(_req: Request, { params }: Params) {
  const { name } = await params;
  const record = getDatabase(name);
  if (!record) {
    return NextResponse.json({ error: "Not found" }, { status: 404 });
  }
  const filePath = getDbPath(name);
  return NextResponse.json({
    ...record,
    file_path: filePath,
    size_bytes: getFileSizeBytes(filePath),
    exists: fs.existsSync(filePath),
  });
}

export async function DELETE(_req: Request, { params }: Params) {
  const { name } = await params;
  const record = getDatabase(name);
  if (!record) {
    return NextResponse.json({ error: "Not found" }, { status: 404 });
  }
  softDeleteDatabase(name);
  return NextResponse.json({ success: true });
}
