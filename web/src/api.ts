import { normalizeWireMetrics, type MetricSeries } from "./lib/metrics";
import type { GridResponse, TestRunDetail } from "./types";

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, init);
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

export interface SuppressionsResponse {
  test_ids: string[];
}

export function getSuppressions(): Promise<SuppressionsResponse> {
  return fetchJSON<SuppressionsResponse>("/api/suppressions");
}

export function addSuppression(testId: string): Promise<SuppressionsResponse> {
  return fetchJSON<SuppressionsResponse>("/api/suppressions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ test_id: testId }),
  });
}

export function removeSuppression(testId: string): Promise<SuppressionsResponse> {
  return fetchJSON<SuppressionsResponse>(
    `/api/suppressions/${encodeURIComponent(testId)}`,
    { method: "DELETE" },
  );
}

export interface DropPlan {
  branch: string;
  origin_url?: string;
  dev: boolean;
  trace_files: number;
  metric_files: number;
  trace_bytes: number;
  metric_bytes: number;
  oldest_run_utc?: string;
  newest_run_utc?: string;
  suppressions_n: number;
  branch_missing: boolean;
  drop_traces: boolean;
  drop_metrics: boolean;
  before_utc?: string;
  nothing: boolean;
}

export interface DropSelector {
  drop_traces: boolean;
  drop_metrics: boolean;
  // Date-only string (YYYY-MM-DD) interpreted as UTC midnight, or
  // omitted to drop everything matching the signal selector.
  before_utc?: string;
}

export function getDropPlan(sel: DropSelector): Promise<DropPlan> {
  const params = new URLSearchParams({
    drop_traces: String(sel.drop_traces),
    drop_metrics: String(sel.drop_metrics),
  });
  if (sel.before_utc) params.set("before_utc", sel.before_utc);
  return fetchJSON<DropPlan>(`/api/drop/plan?${params.toString()}`);
}

export function dropHistory(sel: DropSelector): Promise<DropPlan> {
  return fetchJSON<DropPlan>("/api/drop", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(sel),
  });
}
