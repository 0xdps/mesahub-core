export const dynamic = "force-dynamic";

import { FILES_ENABLED } from "@/lib/features";
import { goFetchAdmin } from "@/lib/go-api";
import { HomeClient } from "./home-client";

export interface DbRecord {
  id: number;
  name: string;
  owner: string;
  status: string;
  created_at: string;
  size_bytes?: number;
}

export interface BucketRecord {
  id: string;
  name: string;
  display_name: string;
  description: string | null;
  status: string;
  size_bytes: number;
  created_at: string;
}

export interface MetricsData {
  volume_used_bytes: number;
  volume_total_bytes: number;
  volume_used_percent: number;
}

const SYSTEM_DBS = new Set(["store"]);

export default async function DashboardPage() {
  const [dbsRes, metricsRes, bucketsRes] = await Promise.all([
    goFetchAdmin("/api/db").catch(() => null),
    goFetchAdmin("/api/metrics").catch(() => null),
    FILES_ENABLED ? goFetchAdmin("/api/buckets").catch(() => null) : Promise.resolve(null),
  ]);

  const allDbs: DbRecord[] = dbsRes?.ok ? await dbsRes.json().catch(() => []) : [];
  const metrics: MetricsData | null = metricsRes?.ok
    ? await metricsRes.json().catch(() => null)
    : null;
  const bucketsRaw: unknown[] = bucketsRes?.ok ? await bucketsRes.json().catch(() => []) : [];

  const buckets: BucketRecord[] = (bucketsRaw as Record<string, unknown>[]).map((r) => ({
    id: String(r.id ?? ""),
    name: String(r.name ?? ""),
    display_name: String(r.display_name ?? ""),
    description: r.description ? String(r.description) : null,
    status: String(r.status ?? ""),
    size_bytes: Number(r.size_bytes ?? 0),
    created_at: String(r.created_at ?? ""),
  }));

  const userDbs = allDbs.filter((d) => !SYSTEM_DBS.has(d.name));
  const systemDbs = allDbs.filter((d) => SYSTEM_DBS.has(d.name));

  return (
    <HomeClient
      userDbs={userDbs}
      systemDbs={systemDbs}
      buckets={buckets}
      metrics={metrics}
    />
  );
}


