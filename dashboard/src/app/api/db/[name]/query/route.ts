import { goFetchDb } from "@/lib/go-api";
import { NextResponse } from "next/server";

interface Params {
  params: Promise<{ name: string }>;
}

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;
  const body = await req.json().catch(() => null);
  const res = await goFetchDb(
    `/api/db/${encodeURIComponent(name)}/query`,
    req,
    { method: "POST", body: JSON.stringify(body), headers: { "Content-Type": "application/json" } }
  );
  const data = await res.json();
  return NextResponse.json(data, { status: res.status });
}
