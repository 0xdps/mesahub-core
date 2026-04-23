import { NextResponse } from "next/server";

export const runtime = "nodejs";

const GO_API_URL = (process.env.GO_API_URL ?? "http://localhost:3000").replace(/\/$/, "");

interface Params {
  params: Promise<{ dbName: string; fileId: string }>;
}

/**
 * Public file shortlink — proxied to Go's /{dbName}/file/{fileId} endpoint.
 * No admin session required; auth (presigned URL / file token) is validated by Go.
 */
export async function GET(req: Request, { params }: Params) {
  const { dbName, fileId } = await params;
  const url = new URL(req.url);
  const res = await fetch(
    `${GO_API_URL}/${encodeURIComponent(dbName)}/file/${encodeURIComponent(fileId)}${url.search}`,
    { headers: { Authorization: req.headers.get("authorization") ?? "" } }
  );
  return new NextResponse(res.body, { status: res.status, headers: res.headers });
}

export async function HEAD(req: Request, { params }: Params) {
  const { dbName, fileId } = await params;
  const url = new URL(req.url);
  const res = await fetch(
    `${GO_API_URL}/${encodeURIComponent(dbName)}/file/${encodeURIComponent(fileId)}${url.search}`,
    { method: "HEAD", headers: { Authorization: req.headers.get("authorization") ?? "" } }
  );
  return new NextResponse(null, { status: res.status, headers: res.headers });
}
