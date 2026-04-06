import { cleanupExpiredFiles } from "@/lib/file-storage";
import { recordAuditEvent, cleanupExpiredFileTokenRevocations } from "@/lib/registry";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

export async function POST(req: Request) {
  const body = await req.json().catch(() => ({}));
  const fileLimitRaw = body?.file_limit;
  const fileLimit = Number.isFinite(fileLimitRaw)
    ? Math.min(Math.max(Number(fileLimitRaw), 1), 10000)
    : 1000;

  const fileCleanup = cleanupExpiredFiles(fileLimit);
  const tokenCleanup = cleanupExpiredFileTokenRevocations();

  recordAuditEvent({
    eventType: "maintenance.cleanup",
    actor: req.headers.get("x-sqlite-hub-admin") === "1" ? "admin_session" : "unknown",
    metadata: {
      file_limit: fileLimit,
      files_scanned: fileCleanup.scanned,
      files_deleted: fileCleanup.deleted,
      token_revocations_deleted: tokenCleanup.deleted,
    },
  });

  return NextResponse.json({
    success: true,
    file_cleanup: fileCleanup,
    token_revocation_cleanup: tokenCleanup,
  });
}
