import fs from "fs";
import { NextResponse } from "next/server";

export async function GET() {
  const DATA_PATH = process.env.DATA_PATH ?? "/data";
  const blobsDir = `${DATA_PATH}/files/blobs`;
  let blobCount = 0;
  let blobsError: string | null = null;
  let sampleBlobs: string[] = [];

  try {
    const entries = fs.readdirSync(blobsDir);
    blobCount = entries.length;
    sampleBlobs = entries.slice(0, 3);
  } catch (e: unknown) {
    blobsError = e instanceof Error ? e.message : String(e);
  }

  return NextResponse.json({
    status: "ok",
    uptime: process.uptime(),
    storage: {
      DATA_PATH,
      blobsDir,
      blobCount,
      sampleBlobs,
      error: blobsError,
    },
  });
}
