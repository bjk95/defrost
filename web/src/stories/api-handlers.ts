import type { GridResponse } from "@/types";
import type { MetricSeries } from "@/lib/metrics";
import { makeTestRunDetail } from "./fixtures";
import type { MockHandler } from "./mock-fetch";

const TEST_RUN_RE = /\/api\/test\/([^/?#]+)\/run\/([^/?#]+)/;

// Build the four handlers the dashboard needs. Order matters: the
// `/api/test/.../run/...` regex must come before `/api/tests` (substring) so
// the run-detail URL doesn't accidentally fall through to the grid.
export function makeApiHandlers(opts: {
  grid: GridResponse;
  metrics?: MetricSeries[];
}): MockHandler[] {
  const handlers: MockHandler[] = [
    {
      match: (u) => TEST_RUN_RE.test(u),
      body: (url: string) => {
        const m = url.match(TEST_RUN_RE);
        const testID = decodeURIComponent(m?.[1] ?? "");
        const runID = decodeURIComponent(m?.[2] ?? "");
        return makeTestRunDetail(testID, runID);
      },
    },
    { match: (u) => u.includes("/api/tests"), body: opts.grid },
    { match: (u) => u.includes("/api/metrics"), body: { metrics: opts.metrics ?? [] } },
  ];
  return handlers;
}
