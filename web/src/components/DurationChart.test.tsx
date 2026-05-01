import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { DurationChart } from "./DurationChart";

describe("DurationChart", () => {
  it("renders with empty points without crashing", () => {
    const { container } = render(
      <DurationChart
        points={[]}
        p50={0}
        p95={0}
        selIdx={0}
        onSelect={vi.fn()}
        onOpen={vi.fn()}
      />,
    );
    expect(container).toBeTruthy();
  });

  it("renders with data points", () => {
    const run = {
      run_id: "r1",
      ts: "2026-01-01T00:00:00Z",
      commit: "abc1234",
      branch: "main",
    };
    const cell = { run_id: "r1", status: "pass" as const, duration_ms: 120 };
    const { container } = render(
      <DurationChart
        points={[{ run, cell }]}
        p50={100}
        p95={200}
        selIdx={0}
        onSelect={vi.fn()}
        onOpen={vi.fn()}
      />,
    );
    expect(container).toBeTruthy();
  });
});
