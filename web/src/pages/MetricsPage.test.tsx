import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { MetricsPage } from "./MetricsPage";
import { screen, waitFor } from "@testing-library/react";
import * as api from "@/api";

vi.mock("@/api");

describe("MetricsPage", () => {
  beforeEach(() => {
    vi.mocked(api.getTests).mockResolvedValue({
      runs: [
        { run_id: "run-2", ts: "2026-01-02T00:00:00Z", commit: "deadbee", branch: "main" },
        { run_id: "run-1", ts: "2026-01-01T00:00:00Z", commit: "cafebab", branch: "main" },
      ],
      tests: [
        {
          test_id: "pkg.TestA",
          test_name: "pkg.TestA",
          cells: [{ run_id: "run-1", status: "pass", duration_ms: 5 }],
        },
      ],
    });
  });

  it("renders the metric the server returns", async () => {
    vi.mocked(api.getMetrics).mockResolvedValue([
      {
        name: "eval.factuality",
        description: "Mean factuality score from the eval harness.",
        unit: "{score}",
        instrument: "gauge",
        points: [
          { run_id: "run-1", ts: "2026-01-01T00:00:00Z", attrs: {}, value: 0.81 },
          { run_id: "run-2", ts: "2026-01-02T00:00:00Z", attrs: {}, value: 0.83 },
        ],
      },
    ]);
    renderWithProviders(<MetricsPage />);
    await waitFor(() => screen.getByText("eval"));
    expect(screen.getByText("factuality")).toBeTruthy();
    expect(screen.getByText(/of 1 metrics/)).toBeTruthy();
  });

  it("shows the empty state when no metrics have been ingested", async () => {
    vi.mocked(api.getMetrics).mockResolvedValue([]);
    renderWithProviders(<MetricsPage />);
    await waitFor(() => screen.getByText(/no metrics ingested yet/i));
  });
});
