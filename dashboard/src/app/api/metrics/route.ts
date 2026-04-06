import { goFetchAdmin } from "@/lib/go-api";
import { NextResponse } from "next/server";

export async function GET() {
  const res = await goFetchAdmin("/api/metrics");
  const data = await res.json();
  return NextResponse.json(data, { status: res.status });
}
