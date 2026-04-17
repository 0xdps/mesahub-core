export const dynamic = "force-dynamic";

import { BUCKETS_ENABLED } from "@/lib/features";
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
  user_id: string;
  name: string;
  display_name: string;
  description: string | null;
  status: string;
  size_bytes: number;
  created_at: string;
}

export interface UserOption {
  id: string;
  email: string;
}

export interface MetricsData {
  volume_used_bytes: number;
  volume_total_bytes: number;
  volume_used_percent: number;
}

const SYSTEM_DBS = new Set(["registry", "control"]);

export default async function DashboardPage() {
  const [dbsRes, metricsRes, bucketsRes, usersRes] = await Promise.all([
    goFetchAdmin("/api/db").catch(() => null),
    goFetchAdmin("/api/metrics").catch(() => null),
    BUCKETS_ENABLED
      ? goFetchAdmin("/api/system/db/control/query", {
          method: "POST",
          body: JSON.stringify({
            sql: `SELECT id, user_id, name, display_name, description, status, size_bytes, created_at
                  FROM buckets WHERE status != 'deleted' ORDER BY created_at DESC LIMIT 500`,
          }),
        }).catch(() => null)
      : Promise.resolve(null),
    goFetchAdmin("/api/system/db/control/query", {
      method: "POST",
      body: JSON.stringify({
        sql: "SELECT id, email FROM users ORDER BY created_at DESC LIMIT 500",
      }),
    }).catch(() => null),
  ]);

  const allDbs: DbRecord[] = dbsRes?.ok ? await dbsRes.json().catch(() => []) : [];
  const metrics: MetricsData | null = metricsRes?.ok
    ? await metricsRes.json().catch(() => null)
    : null;
  const bucketsJson = bucketsRes?.ok ? await bucketsRes.json().catch(() => ({})) : {};
  const usersJson = usersRes?.ok ? await usersRes.json().catch(() => ({})) : {};

  const buckets: BucketRecord[] = ((bucketsJson.rows ?? []) as Record<string, unknown>[]).map(
    (r) => ({
      id: String(r.id ?? ""),
      user_id: String(r.user_id ?? ""),
      name: String(r.name ?? ""),
      display_name: String(r.display_name ?? ""),
      description: r.description ? String(r.description) : null,
      status: String(r.status ?? ""),
      size_bytes: Number(r.size_bytes ?? 0),
      created_at: String(r.created_at ?? ""),
    })
  );

  const users: UserOption[] = ((usersJson.rows ?? []) as Record<string, unknown>[])
    .map((r) => ({ id: String(r.id ?? ""), email: String(r.email ?? "") }))
    .filter((u) => u.id.length > 0);

  const userDbs = allDbs.filter((d) => !SYSTEM_DBS.has(d.name));
  const systemDbs = allDbs.filter((d) => SYSTEM_DBS.has(d.name));

  return (
    <HomeClient
      userDbs={userDbs}
      systemDbs={systemDbs}
      buckets={buckets}
      users={users}
      metrics={metrics}
    />
  );
}


