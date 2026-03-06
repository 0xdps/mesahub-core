export interface ExecMetricsSnapshot {
  totalRequests: number;
  readRequests: number;
  writeRequests: number;
  totalErrors: number;
  sqliteBusyErrors: number;
  totalRowsRead: number;
  totalRowsAffected: number;
  queueWaitMsTotal: number;
  executionMsTotal: number;
}

const _metrics: ExecMetricsSnapshot = {
  totalRequests: 0,
  readRequests: 0,
  writeRequests: 0,
  totalErrors: 0,
  sqliteBusyErrors: 0,
  totalRowsRead: 0,
  totalRowsAffected: 0,
  queueWaitMsTotal: 0,
  executionMsTotal: 0,
};

export function recordExecRequest(type: "read" | "write"): void {
  _metrics.totalRequests += 1;
  if (type === "read") _metrics.readRequests += 1;
  else _metrics.writeRequests += 1;
}

export function recordExecSuccess(stats: { rowsRead?: number; rowsAffected?: number; queueWaitMs?: number; executionMs?: number }): void {
  _metrics.totalRowsRead += stats.rowsRead ?? 0;
  _metrics.totalRowsAffected += stats.rowsAffected ?? 0;
  _metrics.queueWaitMsTotal += stats.queueWaitMs ?? 0;
  _metrics.executionMsTotal += stats.executionMs ?? 0;
}

export function recordExecError(message: string): void {
  _metrics.totalErrors += 1;
  if (/SQLITE_BUSY/i.test(message)) {
    _metrics.sqliteBusyErrors += 1;
  }
}

export function getExecMetricsSnapshot(): ExecMetricsSnapshot {
  return { ..._metrics };
}
