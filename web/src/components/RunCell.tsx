import { useSearchParams } from "react-router-dom";
import { cn } from "@/lib/utils";

const colorByStatus: Record<string, string> = {
  pass: "bg-green-500 hover:ring-2 ring-green-700",
  fail: "bg-red-500 hover:ring-2 ring-red-700",
  skip: "bg-neutral-300 hover:ring-2 ring-neutral-500",
};

export function RunCell({
  testId,
  runId,
  status,
}: {
  testId: string;
  runId: string;
  status: string | null;
}) {
  const [, setParams] = useSearchParams();
  const color = status ? colorByStatus[status] ?? "bg-yellow-500" : "bg-neutral-100 border border-neutral-200";
  return (
    <button
      type="button"
      data-testid={`run-cell-${testId}-${runId}`}
      className={cn("h-3.5 w-3.5 rounded-sm shrink-0", color)}
      onClick={() => {
        setParams({ test: testId, run: runId });
      }}
      aria-label={`run ${runId} test ${testId} status ${status ?? "missing"}`}
    />
  );
}
