import { normalizeWireMetrics, type MetricSeries } from "./lib/metrics";
import type { GridResponse, TestRunDetail } from "./types";

async function fetchJSON<T>(url: string): Promise<T> {
  const r = await fetch(url);
  if (!r.ok) throw new Error(`${url}: ${r.status} ${r.statusText}`);
  return (await r.json()) as T;
}

export function getTests(): Promise<GridResponse> {
  return fetchJSON<GridResponse>("/api/tests");
}

export function getTestRun(testID: string, runID: string): Promise<TestRunDetail> {
  return fetchJSON<TestRunDetail>(
    `/api/test/${encodeURIComponent(testID)}/run/${encodeURIComponent(runID)}`
  );
}

export async function getMetrics(): Promise<MetricSeries[]> {
  const body = await fetchJSON<{ metrics: Parameters<typeof normalizeWireMetrics>[0] }>(
    "/api/metrics",
  );
  return normalizeWireMetrics(body.metrics ?? []);
}
