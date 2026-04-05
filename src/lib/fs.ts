import { execSync } from "child_process";
import fs from "fs";
import path from "path";

const DATA_PATH = process.env.DATA_PATH ?? "/data";

export function getFileSizeBytes(filePath: string): number {
  try {
    return fs.statSync(filePath).size;
  } catch {
    return 0;
  }
}

export function getDbPath(name: string): string {
  // Strictly map name to /data/{name}.db — no path traversal possible
  return path.join(DATA_PATH, `${name}.db`);
}

export function getVolumeUsagePercent(): number {
  try {
    const out = execSync(`df -k "${DATA_PATH}"`).toString();
    const line = out.split("\n")[1];
    if (!line) return 0;
    const match = line.trim().split(/\s+/)[4];
    return parseInt(match?.replace("%", "") ?? "0", 10);
  } catch {
    return 0;
  }
}

export function getVolumeSummary(): { used: number; total: number; usedPercent: number } {
  try {
    const out = execSync(`df -k "${DATA_PATH}"`).toString();
    const parts = out.split("\n")[1]?.trim().split(/\s+/) ?? [];
    const total = parseInt(parts[1] ?? "0", 10) * 1024;
    const used = parseInt(parts[2] ?? "0", 10) * 1024;
    const usedPercent = parseInt((parts[4] ?? "0").replace("%", ""), 10);
    return { used, total, usedPercent };
  } catch {
    return { used: 0, total: 0, usedPercent: 0 };
  }
}
