// Telemetry disabled — no-op stub
export interface TrackEventItem {
  name: string;
  data?: unknown;
}
// eslint-disable-next-line @typescript-eslint/no-unused-vars
export function sendAnalyticEvents(_events: TrackEventItem[]) {}
export function normalizedPathname(pathname: string) { return pathname; }
