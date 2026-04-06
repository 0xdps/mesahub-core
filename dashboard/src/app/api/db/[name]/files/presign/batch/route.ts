import { goFetchDb } from "@/lib/go-api";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string }>;
}

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;
  const body = await req.json().catch(() => ({}));
  const res = await goFetchDb(
    `/api/db/${encodeURIComponent(name)}/files/presign/batch`,
    req,
    { method: "POST", body: JSON.stringify(body), headers: { "Content-Type": "application/json" } }
  );
  const data = await res.json();
  return NextResponse.json(data, { status: res.status });
}
