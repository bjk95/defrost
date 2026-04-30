import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { TestsPage } from "./TestsPage";
import { screen, waitFor } from "@testing-library/react";
import * as api from "@/api";

vi.mock("@/api");

describe("TestsPage", () => {
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
          cells: [
            { run_id: "run-1", status: "pass", duration_ms: 5 },
            { run_id: "run-2", status: "fail", duration_ms: 9 },
          ],
        },
      ],
    });
  });

  it("renders prefix-grouped tests with their history strip", async () => {
    renderWithProviders(<TestsPage />);
    await waitFor(() => screen.getByText("pkg"));
    expect(screen.getByText("TestA")).toBeTruthy();
  });

  it("shows the empty state when no runs exist", async () => {
    vi.mocked(api.getTests).mockResolvedValue({ runs: [], tests: [] });
    renderWithProviders(<TestsPage />);
    await waitFor(() => screen.getByText(/no test runs yet/i));
  });
});
