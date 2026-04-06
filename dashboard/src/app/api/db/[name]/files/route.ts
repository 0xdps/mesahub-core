import { goFetchDb } from "@/lib/go-api";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string }>;
}

export async function GET(req: Request, { params }: Params) {
  const { name } = await params;
  const url = new URL(req.url);
  const res = await goFetchDb(
    `/api/db/${encodeURIComponent(name)}/files${url.search}`,
    req
  );
  const data = await res.json();
  return NextResponse.json(data, { status: res.status });
}

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;
  // Forward multipart/form-data as-is
  const formData = await req.formData();
  const res = await goFetchDb(
    `/api/db/${encodeURIComponent(name)}/files`,
    req,
    { method: "POST", body: formData }
  );
  const data = await res.json();
  return NextResponse.json(data, { status: res.status });
}
