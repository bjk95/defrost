import { describe, it } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { DurationSparkline } from "./DurationSparkline";

describe("DurationSparkline", () => {
  it("renders without throwing on sample data", () => {
    renderWithProviders(
      <DurationSparkline
        cells={[
          { run_id: "run-1", status: "pass", duration_ms: 5 },
          { run_id: "run-2", status: "fail", duration_ms: 9 },
          { run_id: "run-3", status: "pass", duration_ms: 7 },
        ]}
        runs={[
          { run_id: "run-3", ts: "2026-01-03T00:00:00Z" },
          { run_id: "run-2", ts: "2026-01-02T00:00:00Z" },
          { run_id: "run-1", ts: "2026-01-01T00:00:00Z" },
        ]}
        selectedRunId="run-2"
      />
    );
  });

  it("handles an empty cell array", () => {
    renderWithProviders(
      <DurationSparkline cells={[]} runs={[]} selectedRunId="" />
    );
  });
});
