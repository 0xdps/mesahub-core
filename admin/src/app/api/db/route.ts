import { goFetchAdmin } from "@/lib/go-api";
import { NextResponse } from "next/server";

export async function GET() {
  const res = await goFetchAdmin("/api/db");
  const data = await res.json();
  return NextResponse.json(data, { status: res.status });
}

export async function POST(req: Request) {
  const body = await req.json().catch(() => null);
  const res = await goFetchAdmin("/api/db", {
    method: "POST",
    body: JSON.stringify(body),
  });
  const data = await res.json();
  return NextResponse.json(data, { status: res.status });
}
