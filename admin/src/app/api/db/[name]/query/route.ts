import { goFetchDb } from "@/lib/go-api";
import { NextResponse } from "next/server";

interface Params {
  params: Promise<{ name: string }>;
}

export const runtime = "nodejs";

export async function POST(req: Request, { params }: Params) {
  const { name } = await params;
  const rawBody = await req.text();
  const res = await goFetchDb(
    `/api/db/${encodeURIComponent(name)}/query`,
    req,
    {
      method: "POST",
      body: rawBody === "" ? "null" : rawBody,
      headers: { "Content-Type": "application/json" },
    }
  );
  return new NextResponse(res.body, {
    status: res.status,
    headers: {
      "Content-Type": res.headers.get("Content-Type") ?? "application/json",
    },
  });
}
