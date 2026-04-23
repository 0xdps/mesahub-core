import { goFetchAdmin } from "@/lib/go-api";
import { NextResponse } from "next/server";

interface Params {
  params: Promise<{ name: string }>;
}

export async function GET(_req: Request, { params }: Params) {
  const { name } = await params;
  const res = await goFetchAdmin(`/api/db/${encodeURIComponent(name)}`);
  const data = await res.json();
  return NextResponse.json(data, { status: res.status });
}

export async function PATCH(req: Request, { params }: Params) {
  const { name } = await params;
  const body = await req.json().catch(() => null);
  const res = await goFetchAdmin(`/api/db/${encodeURIComponent(name)}`, {
    method: "PATCH",
    body: JSON.stringify(body),
  });
  const data = await res.json();
  return NextResponse.json(data, { status: res.status });
}

export async function DELETE(_req: Request, { params }: Params) {
  const { name } = await params;
  const res = await goFetchAdmin(`/api/db/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
  const data = await res.json().catch(() => null);
  return NextResponse.json(data ?? { success: true }, { status: res.status });
}

