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
    await waitFor(() => screen.getByText(/no tests recorded yet/i));
  });

  // Regression: branch and leaf at the same tree path used to share React
  // keys, so React would drop one of the rows. After the fix, both render.
  it("renders both a leaf and a sibling branch with the same name", async () => {
    vi.mocked(api.getTests).mockResolvedValue({
      runs: [{ run_id: "run-1", ts: "2026-01-01T00:00:00Z" }],
      tests: [
        {
          test_id: "foo",
          test_name: "foo",
          cells: [{ run_id: "run-1", status: "pass", duration_ms: 1 }],
        },
        {
          test_id: "foo/bar",
          test_name: "foo/bar",
          cells: [{ run_id: "run-1", status: "pass", duration_ms: 2 }],
        },
      ],
    });
    const { container } = renderWithProviders(<TestsPage />);
    await waitFor(() => screen.getAllByText("foo"));
    // Two "foo" labels expected: the standalone leaf and the branch header.
    const fooLabels = Array.from(container.querySelectorAll("span")).filter(
      (el) => el.textContent === "foo",
    );
    expect(fooLabels.length).toBe(2);
    expect(screen.getByText("bar")).toBeTruthy();
  });
});
