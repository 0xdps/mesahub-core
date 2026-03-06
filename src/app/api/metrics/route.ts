import { getExecMetricsSnapshot } from "@/lib/exec-metrics";
import { getFileStorageMetrics } from "@/lib/file-storage";
import { getDbPath, getFileSizeBytes, getVolumeSummary } from "@/lib/fs";
import { listDatabases } from "@/lib/registry";
import { NextResponse } from "next/server";

export async function GET() {
  const dbs = listDatabases().filter((d) => d.status === "active");
  const sizes = dbs.map((d) => ({ name: d.name, size_bytes: getFileSizeBytes(getDbPath(d.name)) }));
  const largest = sizes.reduce((a, b) => (b.size_bytes > a.size_bytes ? b : a), { name: "", size_bytes: 0 });
  const volume = getVolumeSummary();
  const files = getFileStorageMetrics();
  const exec = getExecMetricsSnapshot();

  return NextResponse.json({
    total_dbs: dbs.length,
    volume_used_bytes: volume.used,
    volume_total_bytes: volume.total,
    volume_used_percent: volume.usedPercent,
    largest_db: largest.name ? largest : null,
    databases: sizes,
    files: {
      total_files: files.totalFiles,
      total_bytes: files.totalBytes,
      by_database: files.byDatabase,
    },
    exec,
  });
}
