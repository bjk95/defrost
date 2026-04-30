import { LineChart, Line, ReferenceDot, XAxis, YAxis, Tooltip as RTooltip, ResponsiveContainer } from "recharts";
import type { Cell, RunSummary } from "@/types";

export function DurationSparkline({
  cells,
  runs,
  selectedRunId,
}: {
  cells: Cell[];
  runs: RunSummary[];
  selectedRunId: string;
}) {
  // Reverse runs so x-axis goes oldest → newest.
  const ordered = [...runs].reverse();
  const cellByRun = new Map(cells.map((c) => [c.run_id, c]));
  const data = ordered
    .map((r) => ({ run_id: r.run_id, ts: r.ts, duration_ms: cellByRun.get(r.run_id)?.duration_ms ?? null }))
    .filter((p) => p.duration_ms !== null) as Array<{ run_id: string; ts: string; duration_ms: number }>;

  if (data.length === 0) {
    return <p className="text-xs text-muted-foreground">no duration data</p>;
  }

  const selected = data.find((p) => p.run_id === selectedRunId);

  return (
    <div className="h-24 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 8, bottom: 8, left: 0, right: 0 }}>
          <XAxis dataKey="ts" hide />
          <YAxis hide domain={["dataMin", "dataMax"]} />
          <RTooltip
            formatter={(v) => [`${v} ms`, "duration"]}
            labelFormatter={(_, payload) => payload?.[0]?.payload?.run_id ?? ""}
          />
          <Line type="monotone" dataKey="duration_ms" stroke="var(--primary)" strokeWidth={1.5} dot={false} isAnimationActive={false} />
          {selected && (
            <ReferenceDot x={selected.ts} y={selected.duration_ms} r={4} fill="var(--primary)" />
          )}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
