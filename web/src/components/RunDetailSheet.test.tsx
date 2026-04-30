import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { RunDetailSheet } from "./RunDetailSheet";
import { screen, waitFor } from "@testing-library/react";
import * as api from "@/api";

vi.mock("@/api");

describe("RunDetailSheet", () => {
  beforeEach(() => {
    vi.mocked(api.getTestRun).mockResolvedValue({
      test: {
        schema: 2,
        test_id: "tid-A",
        test_name: "pkg.TestA",
        run_id: "run-2",
        ts: "2026-01-02T00:00:00Z",
        ran: true,
        passed: false,
        status: "fail",
        duration_ms: 12,
        output: "FAIL\n  expected 1, got 2\n",
      },
      run: {
        schema: 2,
        run_id: "run-2",
        commit: "deadbee",
        branch: "main",
        ts: "2026-01-02T00:00:00Z",
        dirty: false,
      },
    });
    vi.mocked(api.getTests).mockResolvedValue({
      runs: [{ run_id: "run-2", ts: "2026-01-02T00:00:00Z" }],
      tests: [
        {
          test_id: "tid-A",
          test_name: "pkg.TestA",
          cells: [{ run_id: "run-2", status: "fail", duration_ms: 12 }],
        },
      ],
    });
  });

  it("opens when ?run=&test= is set and renders output + commit", async () => {
    renderWithProviders(<RunDetailSheet />, {
      router: { initialEntries: ["/?run=run-2&test=tid-A"] },
    });
    await waitFor(() => screen.getByText(/expected 1, got 2/));
    expect(screen.getByText(/deadbee/)).toBeTruthy();
    expect(screen.getByText(/pkg\.TestA/)).toBeTruthy();
  });

  it("renders nothing when no params are set", () => {
    const { container } = renderWithProviders(<RunDetailSheet />, {
      router: { initialEntries: ["/"] },
    });
    expect(container.querySelector("[data-state='open']")).toBeNull();
  });
});
