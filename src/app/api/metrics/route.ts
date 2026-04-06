import { getExecMetricsSnapshot } from "@/lib/exec-metrics";
import { getFileStorageMetrics } from "@/lib/file-storage";
import { getDbPath, getFileSizeBytes, getVolumeSummary } from "@/lib/fs";
import { getAuditMetrics, listDatabases } from "@/lib/registry";
import { NextResponse } from "next/server";

export async function GET() {
  const dbs = listDatabases().filter((d) => d.status === "active");
  const sizes = dbs.map((d) => ({ name: d.name, size_bytes: getFileSizeBytes(getDbPath(d.name)) }));
  const largest = sizes.reduce((a, b) => (b.size_bytes > a.size_bytes ? b : a), { name: "", size_bytes: 0 });
  const volume = getVolumeSummary();
  const files = getFileStorageMetrics();
  const exec = getExecMetricsSnapshot();
  const audit = getAuditMetrics();

  const totalRequests = exec.totalRequests || 0;
  const totalErrors = exec.totalErrors || 0;
  const errorRate = totalRequests > 0 ? totalErrors / totalRequests : 0;
  const availabilityEstimate = totalRequests > 0 ? 1 - errorRate : 1;

  const slo = {
    availability_estimate_pct: Number((availabilityEstimate * 100).toFixed(2)),
    error_rate_pct: Number((errorRate * 100).toFixed(2)),
    sqlite_busy_error_rate_pct:
      totalRequests > 0 ? Number(((exec.sqliteBusyErrors / totalRequests) * 100).toFixed(2)) : 0,
    avg_execution_ms: totalRequests > 0 ? Number((exec.executionMsTotal / totalRequests).toFixed(2)) : 0,
    avg_queue_wait_ms: totalRequests > 0 ? Number((exec.queueWaitMsTotal / totalRequests).toFixed(2)) : 0,
  };

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
    slo,
    audit,
  });
}
