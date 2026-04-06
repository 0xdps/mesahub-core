import { goFetchDb } from "@/lib/go-api";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string; id: string }>;
}

export async function GET(req: Request, { params }: Params) {
  const { name, id } = await params;
  const url = new URL(req.url);
  const res = await goFetchDb(
    `/api/db/${encodeURIComponent(name)}/files/${encodeURIComponent(id)}/meta${url.search}`,
    req
  );
  const data = await res.json();
  return NextResponse.json(data, { status: res.status });
}
