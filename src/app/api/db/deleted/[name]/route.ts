import { logger } from "@/lib/logger";
import { getDatabase, hardDeleteDatabase, restoreDatabase } from "@/lib/registry";
import { NextResponse } from "next/server";

const ADMIN_SESSION_HEADER = "x-sqlite-hub-admin";

interface Params {
  params: Promise<{ name: string }>;
}

export async function POST(req: Request, { params }: Params) {
  if (req.headers.get(ADMIN_SESSION_HEADER) !== "1") {
    return NextResponse.json({ error: "Admin session required" }, { status: 401 });
  }

  const { name } = await params;

  const record = getDatabase(name);
  if (!record || record.status !== "deleted") {
    return NextResponse.json({ error: "Deleted database not found" }, { status: 404 });
  }

  const body = await req.json().catch(() => null);
  const action = body?.action;

  if (action === "restore") {
    // Check if a DB with the original name already exists (active)
    const originalName = record.original_name;
    if (!originalName) {
      return NextResponse.json({ error: "Missing original name" }, { status: 400 });
    }
    const existing = getDatabase(originalName);
    if (existing) {
      return NextResponse.json(
        { error: `A database named "${originalName}" already exists. Delete or rename it first.` },
        { status: 409 }
      );
    }

    try {
      const restored = restoreDatabase(name);
      logger.info(`[db] Restored deleted database "${name}" → "${restored.name}"`);
      return NextResponse.json(restored);
    } catch (err) {
      logger.warn(`[db] Restore failed for "${name}": ${(err as Error).message}`);
      return NextResponse.json({ error: (err as Error).message }, { status: 400 });
    }
  }

  if (action === "hard_delete") {
    try {
      hardDeleteDatabase(name);
      logger.info(`[db] Permanently deleted database "${name}"`);
      return NextResponse.json({ success: true });
    } catch (err) {
      logger.warn(`[db] Hard delete failed for "${name}": ${(err as Error).message}`);
      return NextResponse.json({ error: (err as Error).message }, { status: 400 });
    }
  }

  return NextResponse.json({ error: "Invalid action. Use 'restore' or 'hard_delete'" }, { status: 400 });
}
