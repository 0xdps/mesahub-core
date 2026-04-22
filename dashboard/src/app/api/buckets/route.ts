import { randomUUID } from "crypto";

import { FILES_ENABLED } from "@/lib/features";
import { goFetchAdmin } from "@/lib/go-api";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface CreateBucketBody {
  userId?: unknown;
  name?: unknown;
  displayName?: unknown;
  description?: unknown;
}

function isValidBucketName(name: string) {
  return /^[a-z0-9_-]+$/.test(name);
}

export async function POST(req: Request) {
  if (!FILES_ENABLED) {
    return NextResponse.json({ error: "File storage is disabled" }, { status: 404 });
  }
  const body = (await req.json().catch(() => null)) as CreateBucketBody | null;

  const userId = typeof body?.userId === "string" ? body.userId.trim() : "";
  const name = typeof body?.name === "string" ? body.name.trim() : "";
  const displayNameRaw =
    typeof body?.displayName === "string" ? body.displayName.trim() : "";
  const displayName = displayNameRaw || name;
  const description =
    typeof body?.description === "string" && body.description.trim() !== ""
      ? body.description.trim()
      : null;

  if (!userId) {
    return NextResponse.json({ error: "userId is required" }, { status: 400 });
  }
  if (!name) {
    return NextResponse.json({ error: "name is required" }, { status: 400 });
  }
  if (!isValidBucketName(name)) {
    return NextResponse.json(
      {
        error:
          "name must contain only lowercase letters, numbers, hyphens, or underscores",
      },
      { status: 400 }
    );
  }
  if (!displayName) {
    return NextResponse.json(
      { error: "displayName is required" },
      { status: 400 }
    );
  }

  const sql = `INSERT INTO buckets (id, user_id, name, display_name, description, status)
               VALUES (?, ?, ?, ?, ?, 'active')`;

  const res = await goFetchAdmin("/api/db/control/exec", {
    method: "POST",
    body: JSON.stringify({
      sql,
      bindings: [randomUUID(), userId, name, displayName, description],
    }),
  });

  const responseText = await res.text();
  let data: unknown = null;
  try {
    data = responseText ? JSON.parse(responseText) : null;
  } catch {
    data = { error: responseText || "Unknown error" };
  }

  return NextResponse.json(data, { status: res.status });
}
