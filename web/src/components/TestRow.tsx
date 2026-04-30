import type { RunSummary, TestRow as TestRowData } from "@/types";
import { RunCell } from "./RunCell";

export function TestRow({
  row,
  runs,
}: {
  row: TestRowData;
  runs: RunSummary[];
}) {
  const cellByRun = new Map(row.cells.map((c) => [c.run_id, c]));
  return (
    <div className="flex items-center gap-2 py-1 font-mono text-xs">
      <div className="flex-1 truncate" title={row.test_name}>
        {row.test_name}
      </div>
      <div className="flex gap-0.5">
        {runs.map((r) => {
          const cell = cellByRun.get(r.run_id);
          return (
            <RunCell
              key={r.run_id}
              testId={row.test_id}
              runId={r.run_id}
              status={cell?.status ?? null}
            />
          );
        })}
      </div>
    </div>
  );
}
