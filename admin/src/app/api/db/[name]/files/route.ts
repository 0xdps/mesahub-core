import { goFetchDb } from "@/lib/go-api";
import { NextResponse } from "next/server";

export const runtime = "nodejs";

interface Params {
  params: Promise<{ name: string }>;
}

function normalizeListPayload(payload: any) {
  const rawFiles = Array.isArray(payload?.files)
    ? payload.files
    : Array.isArray(payload?.Files)
      ? payload.Files
      : [];

  const files = rawFiles.map((f: any) => ({
    id: f?.id ?? f?.ID,
    filename: f?.filename ?? f?.Filename,
    folder_path: f?.folder_path ?? f?.FolderPath ?? "",
    content_type: f?.content_type ?? f?.ContentType ?? null,
    size_bytes: Number(f?.size_bytes ?? f?.SizeBytes ?? 0),
    uploaded_at: f?.uploaded_at ?? f?.UploadedAt,
    expires_at: f?.expires_at ?? f?.ExpiresAt ?? null,
  }));

  return {
    files,
    total: Number(payload?.total ?? payload?.Total ?? files.length),
    offset: Number(payload?.offset ?? payload?.Offset ?? 0),
    limit: Number(payload?.limit ?? payload?.Limit ?? files.length),
  };
}

export async function GET(req: Request, { params }: Params) {
  const { name } = await params;
  const url = new URL(req.url);
  const res = await goFetchDb(
    `/api/db/${encodeURIComponent(name)}/files${url.search}`,
    req
  );
  const data = await res.json();
  if (!res.ok) return NextResponse.json(data, { status: res.status });
  return NextResponse.json(normalizeListPayload(data), { status: res.status });
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
