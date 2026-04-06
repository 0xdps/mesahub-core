import { goFetchDb } from "@/lib/go-api";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string; id: string }>;
}

export async function OPTIONS() {
  return new NextResponse(null, {
    status: 204,
    headers: {
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Methods": "GET, HEAD, OPTIONS",
      "Access-Control-Allow-Headers": "Authorization, Content-Type",
      "Access-Control-Max-Age": "86400",
    },
  });
}

export async function HEAD(req: Request, { params }: Params) {
  const { name, id } = await params;
  const url = new URL(req.url);
  const res = await goFetchDb(
    `/api/buckets/${encodeURIComponent(name)}/files/${encodeURIComponent(id)}${url.search}`,
    req,
    { method: "HEAD" }
  );
  return new NextResponse(null, { status: res.status, headers: res.headers });
}

export async function GET(req: Request, { params }: Params) {
  const { name, id } = await params;
  const url = new URL(req.url);
  const res = await goFetchDb(
    `/api/buckets/${encodeURIComponent(name)}/files/${encodeURIComponent(id)}${url.search}`,
    req
  );
  return new NextResponse(res.body, { status: res.status, headers: res.headers });
}

export async function DELETE(req: Request, { params }: Params) {
  const { name, id } = await params;
  const res = await goFetchDb(
    `/api/buckets/${encodeURIComponent(name)}/files/${encodeURIComponent(id)}`,
    req,
    { method: "DELETE" }
  );
  if (res.status === 204) return new NextResponse(null, { status: 204 });
  const data = await res.json().catch(() => null);
  return NextResponse.json(data ?? { success: true }, { status: res.status });
}
