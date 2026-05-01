import type { Meta, StoryObj } from "@storybook/react-vite";
import { useMemo, useState } from "react";
import { DurationChart, type ChartPoint } from "./DurationChart";
import { testStats } from "@/lib/utils";
import { makeRuns } from "@/stories/fixtures";

const meta = {
  title: "DurationChart",
  parameters: { layout: "padded" },
} satisfies Meta;
export default meta;

type Story = StoryObj;

function buildPoints(
  shape: (i: number) => { status: "pass" | "fail" | "skip"; ms: number },
  count = 20,
): ChartPoint[] {
  const runs = makeRuns(count);
  // runs are newest-first; chart wants oldest-left, so reverse.
  return [...runs].reverse().map((run, i) => {
    const { status, ms } = shape(i);
    return { run, cell: { run_id: run.run_id, status, duration_ms: ms } };
  });
}

function ChartFrame({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        border: "1px solid var(--border)",
        borderRadius: 10,
        background: "var(--bg)",
        padding: 20,
        maxWidth: 920,
      }}
    >
      {children}
    </div>
  );
}

export const SteadyPasses: Story = {
  name: "DurationChart · steady passing",
  render: () => {
    const points = useMemo(
      () =>
        buildPoints((i) => ({
          status: "pass",
          ms: 240 + Math.sin(i * 0.7) * 30 + (i % 3) * 6,
        })),
      [],
    );
    const stats = useMemo(() => testStats(points.map((p) => p.cell)), [points]);
    const [sel, setSel] = useState(points.length - 1);
    return (
      <ChartFrame>
        <DurationChart
          points={points}
          p50={stats.p50}
          p95={stats.p95}
          selIdx={sel}
          onSelect={setSel}
          onOpen={() => undefined}
        />
      </ChartFrame>
    );
  },
};

export const FailuresAndSkips: Story = {
  name: "DurationChart · with failures and skips",
  render: () => {
    const points = useMemo(
      () =>
        buildPoints((i) => {
          if (i === 5 || i === 13) return { status: "fail", ms: 480 + i * 12 };
          if (i === 1 || i === 8) return { status: "skip", ms: 0 };
          return { status: "pass", ms: 220 + Math.sin(i * 0.6) * 38 + (i % 4) * 7 };
        }),
      [],
    );
    const stats = useMemo(() => testStats(points.map((p) => p.cell)), [points]);
    const [sel, setSel] = useState(13);
    return (
      <ChartFrame>
        <DurationChart
          points={points}
          p50={stats.p50}
          p95={stats.p95}
          selIdx={sel}
          onSelect={setSel}
          onOpen={() => undefined}
        />
      </ChartFrame>
    );
  },
};

export const RegressionTrend: Story = {
  name: "DurationChart · regression trend",
  render: () => {
    const points = useMemo(
      () =>
        buildPoints((i) => ({
          status: "pass",
          ms: 80 + i * 14 + Math.sin(i * 1.1) * 16,
        })),
      [],
    );
    const stats = useMemo(() => testStats(points.map((p) => p.cell)), [points]);
    const [sel, setSel] = useState(points.length - 1);
    return (
      <ChartFrame>
        <DurationChart
          points={points}
          p50={stats.p50}
          p95={stats.p95}
          selIdx={sel}
          onSelect={setSel}
          onOpen={() => undefined}
        />
      </ChartFrame>
    );
  },
};

export const SparseHistory: Story = {
  name: "DurationChart · only 4 runs",
  render: () => {
    const points = useMemo(
      () =>
        buildPoints(
          (i) => ({ status: i === 1 ? "fail" : "pass", ms: 200 + i * 30 }),
          4,
        ),
      [],
    );
    const stats = useMemo(() => testStats(points.map((p) => p.cell)), [points]);
    const [sel, setSel] = useState(2);
    return (
      <ChartFrame>
        <DurationChart
          points={points}
          p50={stats.p50}
          p95={stats.p95}
          selIdx={sel}
          onSelect={setSel}
          onOpen={() => undefined}
        />
      </ChartFrame>
    );
  },
};
