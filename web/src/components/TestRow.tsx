import type { TestRow as TestRowData } from "@/types";
import { RunCell } from "./RunCell";

// TestRow renders one test's history. Cells come pre-sorted from the
// server (oldest first). We pad the left with empty placeholders so all
// rows right-align to the same column count, making the newest column
// visually consistent across rows.
export function TestRow({
  row,
  columns,
}: {
  row: TestRowData;
  columns: number;
}) {
  const padding = Math.max(0, columns - row.cells.length);
  return (
    <div className="flex items-center gap-2 py-1 font-mono text-xs">
      <div className="flex-1 truncate" title={row.test_name}>
        {row.test_name}
      </div>
      <div className="flex gap-0.5 justify-end">
        {Array.from({ length: padding }).map((_, i) => (
          <RunCell
            key={`pad-${i}`}
            testId={row.test_id}
            runId={`__pad_${i}`}
            status={null}
            disabled
          />
        ))}
        {row.cells.map((cell) => (
          <RunCell
            key={cell.run_id}
            testId={row.test_id}
            runId={cell.run_id}
            status={cell.status}
          />
        ))}
      </div>
    </div>
  );
}
