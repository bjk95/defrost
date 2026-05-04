import type { Meta, StoryObj } from "@storybook/react-vite";
import {
  Avatar,
  Card,
  CountsBar,
  GroupHistoryStrip,
  HistoryStrip,
  MetaPill,
  RunCell,
  SectionLabel,
  StatusPill,
} from "./Primitives";
import { Icon } from "./Icons";
import { makeGrid, makeRuns } from "@/stories/fixtures";

const meta = {
  title: "Primitives",
} satisfies Meta;
export default meta;

type Story = StoryObj;

function Row({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        display: "flex",
        gap: 16,
        alignItems: "center",
        padding: "12px 0",
      }}
    >
      {children}
    </div>
  );
}

function Label({ children }: { children: React.ReactNode }) {
  return (
    <span
      style={{
        fontFamily: "var(--font-mono)",
        fontSize: 11,
        color: "var(--fg-muted)",
        width: 110,
        display: "inline-block",
      }}
    >
      {children}
    </span>
  );
}

export const StatusPillVariants: Story = {
  name: "StatusPill · all variants",
  render: () => (
    <div>
      {(["pass", "fail", "skip", "flaky", "running", "suppressed"] as const).map(
        (s) => (
          <Row key={s}>
            <Label>{s}</Label>
            <StatusPill status={s} size="xs" />
            <StatusPill status={s} />
          </Row>
        ),
      )}
    </div>
  ),
};

export const RunCellStates: Story = {
  name: "RunCell · status × size",
  render: () => (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {([12, 16, 20] as const).map((size) => (
        <div key={size} style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <Label>size {size}</Label>
          <RunCell status="pass" size={size} title="passing" />
          <RunCell status="fail" size={size} title="failing" />
          <RunCell status="skip" size={size} title="skipped" />
          <RunCell status="pass" size={size} selected title="selected" />
          <RunCell size={size} title="empty / no data" />
        </div>
      ))}
    </div>
  ),
};

export const CountsBarDistributions: Story = {
  name: "CountsBar · distributions",
  render: () => (
    <div>
      {[
        { label: "all pass", counts: { pass: 50, fail: 0, skip: 0 } },
        { label: "mostly pass", counts: { pass: 42, fail: 6, skip: 2 } },
        { label: "half failing", counts: { pass: 25, fail: 25, skip: 0 } },
        { label: "all fail", counts: { pass: 0, fail: 50, skip: 0 } },
        { label: "all skip", counts: { pass: 0, fail: 0, skip: 50 } },
        { label: "empty", counts: { pass: 0, fail: 0, skip: 0 } },
      ].map((row) => (
        <Row key={row.label}>
          <Label>{row.label}</Label>
          <CountsBar counts={row.counts} width={160} />
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: "var(--fg-muted)",
            }}
          >
            {row.counts.pass}/{row.counts.fail}/{row.counts.skip}
          </span>
        </Row>
      ))}
    </div>
  ),
};

export const AvatarVariants: Story = {
  name: "Avatar · initials + sizes",
  render: () => (
    <div style={{ display: "flex", gap: 24, alignItems: "flex-end" }}>
      {[
        { name: "Beth Kemp", size: 18 },
        { name: "octocat", size: 24 },
        { name: "Anonymous Coward", size: 32 },
        { name: undefined, size: 24 },
      ].map((p, i) => (
        <div
          key={i}
          style={{ display: "flex", flexDirection: "column", gap: 6, alignItems: "center" }}
        >
          <Avatar name={p.name} size={p.size} />
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: "var(--fg-muted)",
            }}
          >
            {p.name ?? "—"} · {p.size}
          </span>
        </div>
      ))}
    </div>
  ),
};

export const MetaPillVariants: Story = {
  name: "MetaPill · combinations",
  render: () => (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 8, maxWidth: 720 }}>
      <MetaPill icon={<Icon.GitCommit />} value="deadbee" mono />
      <MetaPill icon={<Icon.GitBranch />} value="main" mono />
      <MetaPill icon={<Icon.GitPullRequest />} value="#1247" mono />
      <MetaPill icon={<Icon.User />} value="Beth Kemp" />
      <MetaPill icon={<Icon.Cpu />} value="darwin/arm64" mono />
      <MetaPill icon={<Icon.Clock />} value="2.4s" mono />
      <MetaPill label="commit" value="deadbee" mono />
      <MetaPill label="model" value="claude-opus-4-7" />
    </div>
  ),
};

export const SectionLabelVariants: Story = {
  name: "SectionLabel · with + without right slot",
  render: () => (
    <div style={{ display: "flex", flexDirection: "column", gap: 24, maxWidth: 540 }}>
      <div>
        <SectionLabel>Run history</SectionLabel>
        <Card padding={12}>
          <span style={{ fontSize: 12, color: "var(--fg-muted)" }}>card body</span>
        </Card>
      </div>
      <div>
        <SectionLabel
          right={
            <span
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 11,
                color: "var(--fg-muted)",
              }}
            >
              20 runs
            </span>
          }
        >
          Duration · 20 runs
        </SectionLabel>
        <Card padding={12}>
          <span style={{ fontSize: 12, color: "var(--fg-muted)" }}>card body</span>
        </Card>
      </div>
    </div>
  ),
};

export const CardVariants: Story = {
  name: "Card · padding + hover",
  render: () => (
    <div style={{ display: "flex", flexDirection: "column", gap: 16, maxWidth: 540 }}>
      <Card>
        <div style={{ fontSize: 13 }}>default — 16px padding</div>
      </Card>
      <Card padding={24}>
        <div style={{ fontSize: 13 }}>roomy — 24px padding</div>
      </Card>
      <Card padding={8}>
        <div style={{ fontSize: 12, color: "var(--fg-muted)" }}>compact — 8px</div>
      </Card>
      <Card hover onClick={() => undefined}>
        <div style={{ fontSize: 13 }}>hover · click — interactive</div>
      </Card>
    </div>
  ),
};

export const HistoryStripVariants: Story = {
  name: "HistoryStrip · per-test rows",
  render: () => {
    const grid = makeGrid();
    const samples = grid.tests.slice(0, 5);
    return (
      <div style={{ display: "flex", flexDirection: "column", gap: 10, maxWidth: 720 }}>
        {samples.map((t) => (
          <div
            key={t.test_id}
            style={{
              display: "grid",
              gridTemplateColumns: "minmax(0,1fr) auto",
              gap: 16,
              alignItems: "center",
            }}
          >
            <span
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 12,
                color: "var(--fg-muted)",
                whiteSpace: "nowrap",
                overflow: "hidden",
                textOverflow: "ellipsis",
              }}
            >
              {t.test_name}
            </span>
            <HistoryStrip row={t} runs={grid.runs} cellSize={11} gap={3} />
          </div>
        ))}
      </div>
    );
  },
};

export const GroupHistoryStripVariants: Story = {
  name: "GroupHistoryStrip · package roll-ups",
  render: () => {
    const runs = makeRuns(20);
    const cases = [
      {
        label: "all green",
        cells: runs.map((r) => ({
          run_id: r.run_id,
          status: "pass" as const,
          duration_ms: 50,
        })),
      },
      {
        label: "one fail",
        cells: runs.map((r, i) => ({
          run_id: r.run_id,
          status: (i === 4 ? "fail" : "pass") as "pass" | "fail",
          duration_ms: 50,
        })),
      },
      {
        label: "scattered",
        cells: runs.map((r, i) => ({
          run_id: r.run_id,
          status: (i % 5 === 0 ? "fail" : i % 7 === 0 ? "skip" : "pass") as
            | "pass"
            | "fail"
            | "skip",
          duration_ms: 50,
        })),
      },
    ];
    return (
      <div style={{ display: "flex", flexDirection: "column", gap: 18, maxWidth: 720 }}>
        {cases.map((c) => (
          <div key={c.label} style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            <span
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 11,
                color: "var(--fg-muted)",
              }}
            >
              {c.label} · default
            </span>
            <GroupHistoryStrip runs={runs} cells={c.cells} />
          </div>
        ))}
      </div>
    );
  },
};
