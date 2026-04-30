import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import {
  Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription,
} from "@/components/ui/sheet";
import { StatusBadge } from "./StatusBadge";
import { DurationSparkline } from "./DurationSparkline";
import { getTestRun, getTests } from "@/api";

export function RunDetailSheet() {
  const [params, setParams] = useSearchParams();
  const tid = params.get("test") ?? "";
  const rid = params.get("run") ?? "";
  const open = Boolean(tid && rid);

  const detail = useQuery({
    queryKey: ["test", tid, "run", rid],
    queryFn: () => getTestRun(tid, rid),
    enabled: open,
    staleTime: Infinity,
  });

  const grid = useQuery({
    queryKey: ["tests"],
    queryFn: getTests,
    enabled: open,
  });

  const close = () => {
    const next = new URLSearchParams(params);
    next.delete("test");
    next.delete("run");
    setParams(next);
  };

  return (
    <Sheet open={open} onOpenChange={(v) => { if (!v) close(); }}>
      <SheetContent className="w-[640px] sm:max-w-[640px] overflow-y-auto p-6">
        {detail.isError && (
          <p className="text-red-600">failed to load: {(detail.error as Error).message}</p>
        )}
        {detail.isPending && !detail.isError && (
          <p className="text-sm text-muted-foreground">loading…</p>
        )}
        {detail.data && (
          <>
            <SheetHeader>
              <SheetTitle className="font-mono text-base">{detail.data.test.test_name}</SheetTitle>
              <SheetDescription>
                <span className="flex items-center gap-2">
                  <StatusBadge status={detail.data.test.status} />
                  <span>{detail.data.test.duration_ms}ms</span>
                  {detail.data.run.commit && (
                    <span className="font-mono text-xs">{detail.data.run.commit.slice(0, 7)}</span>
                  )}
                  {detail.data.run.branch && (
                    <span className="text-xs text-muted-foreground">{detail.data.run.branch}</span>
                  )}
                </span>
              </SheetDescription>
            </SheetHeader>

            <section className="mt-6">
              <h3 className="mb-2 text-sm font-medium">duration over recent runs</h3>
              {grid.data && (() => {
                const row = grid.data.tests.find((t) => t.test_id === tid);
                return row ? (
                  <DurationSparkline cells={row.cells} runs={grid.data.runs} selectedRunId={rid} />
                ) : null;
              })()}
            </section>

            <section className="mt-6">
              <h3 className="mb-2 text-sm font-medium">output</h3>
              <pre className="rounded bg-muted p-3 text-xs whitespace-pre-wrap font-mono">
                {detail.data.test.output ?? "(no output)"}
              </pre>
            </section>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
