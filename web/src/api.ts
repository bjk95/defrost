import type { GridResponse, SuppressionsResponse, TestRunDetail } from "./types";

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

export function getSuppressions(): Promise<SuppressionsResponse> {
  return fetchJSON<SuppressionsResponse>("/api/suppressions");
}

export function addSuppression(testID: string): Promise<SuppressionsResponse> {
  return fetchJSON<SuppressionsResponse>("/api/suppressions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ test_id: testID }),
  });
}

export function removeSuppression(testID: string): Promise<SuppressionsResponse> {
  return fetchJSON<SuppressionsResponse>("/api/suppressions", {
    method: "DELETE",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ test_id: testID }),
  });
}
