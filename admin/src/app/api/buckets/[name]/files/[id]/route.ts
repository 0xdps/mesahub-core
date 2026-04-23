import { goFetchDb } from "@/lib/go-api";
import { NextResponse } from "next/server";
import fs from "fs/promises";
import path from "path";

export const runtime = "nodejs";

// Headers that must not be forwarded from an upstream proxy response.
// Forwarding these causes length/encoding conflicts that abort the stream.
const HOP_BY_HOP = new Set([
  "transfer-encoding",
  "connection",
  "keep-alive",
  "proxy-authenticate",
  "proxy-authorization",
  "te",
  "trailer",
  "upgrade",
]);

function forwardHeaders(src: Headers): Headers {
  const out = new Headers();
  src.forEach((value, key) => {
    if (!HOP_BY_HOP.has(key.toLowerCase())) {
      out.set(key, value);
    }
  });
  return out;
}

// When EnableFileProxyDelivery is true, Go returns 0 bytes with X-Sendfile
// pointing to just the blob filename. Since both services share the same
// container + /data volume, we read the blob directly from disk.
async function handleXSendfile(res: Response): Promise<NextResponse | null> {
  const sendfile = res.headers.get("x-sendfile");
  if (!sendfile) return null;

  const blobName = path.basename(sendfile); // strip any leading slash
  const dataPath = process.env.DATA_PATH ?? "/data";
  const blobPath = path.join(dataPath, "files", "blobs", blobName);

  try {
    const data = await fs.readFile(blobPath);
    const hdrs = forwardHeaders(res.headers);
    hdrs.delete("x-sendfile");
    hdrs.set("content-length", String(data.byteLength));
    return new NextResponse(data, { status: 200, headers: hdrs });
  } catch {
    return new NextResponse(null, { status: 404 });
  }
}

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
  return new NextResponse(null, { status: res.status, headers: forwardHeaders(res.headers) });
}

export async function GET(req: Request, { params }: Params) {
  const { name, id } = await params;
  const url = new URL(req.url);
  const res = await goFetchDb(
    `/api/buckets/${encodeURIComponent(name)}/files/${encodeURIComponent(id)}${url.search}`,
    req
  );

  if (!res.ok) {
    const data = await res.json().catch(() => null);
    return NextResponse.json(data ?? { error: "Not found" }, { status: res.status });
  }

  // When EnableFileProxyDelivery=true, Go returns 0 bytes + X-Sendfile header.
  // Serve the blob directly from the shared /data volume instead.
  const fromDisk = await handleXSendfile(res);
  if (fromDisk) return fromDisk;

  // Normal mode: Go streamed the body — buffer it to avoid pipe-race on close.
  const body = await res.arrayBuffer();
  return new NextResponse(body, { status: res.status, headers: forwardHeaders(res.headers) });
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
