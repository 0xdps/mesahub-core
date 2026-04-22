import { FILES_ENABLED } from "@/lib/features";
import { goFetchAdmin } from "@/lib/go-api";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface CreateBucketBody {
  name?: unknown;
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

  const name = typeof body?.name === "string" ? body.name.trim() : "";
  const description =
    typeof body?.description === "string" && body.description.trim() !== ""
      ? body.description.trim()
      : null;

  if (!name) {
    return NextResponse.json({ error: "name is required" }, { status: 400 });
  }
  if (!isValidBucketName(name)) {
    return NextResponse.json(
      { error: "name must contain only lowercase letters, numbers, hyphens, or underscores" },
      { status: 400 }
    );
  }

  const res = await goFetchAdmin("/api/buckets", {
    method: "POST",
    body: JSON.stringify({ name, display_name: name, description }),
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
