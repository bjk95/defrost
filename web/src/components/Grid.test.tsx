import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { Grid } from "./Grid";
import { screen, waitFor } from "@testing-library/react";
import * as api from "@/api";

vi.mock("@/api");

describe("Grid", () => {
  beforeEach(() => {
    vi.mocked(api.getTests).mockResolvedValue({
      runs: [
        { run_id: "run-2", ts: "2026-01-02T00:00:00Z" },
        { run_id: "run-1", ts: "2026-01-01T00:00:00Z" },
      ],
      tests: [
        {
          test_id: "tid-A",
          test_name: "pkg.TestA",
          cells: [
            { run_id: "run-1", status: "pass", duration_ms: 5 },
            { run_id: "run-2", status: "fail", duration_ms: 9 },
          ],
        },
      ],
    });
  });

  it("renders one row per test with cells in run order", async () => {
    renderWithProviders(<Grid />);
    await waitFor(() => screen.getByText("pkg.TestA"));
    expect(screen.getByTestId("run-cell-tid-A-run-1")).toBeTruthy();
    expect(screen.getByTestId("run-cell-tid-A-run-2")).toBeTruthy();
  });

  it("shows empty state when no runs", async () => {
    vi.mocked(api.getTests).mockResolvedValue({ runs: [], tests: [] });
    renderWithProviders(<Grid />);
    await waitFor(() => screen.getByText(/no test runs yet/i));
  });
});
