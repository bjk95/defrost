import type { Meta, StoryObj } from "@storybook/react-vite";
import { Avatar, CountsBar, RunCell, StatusPill } from "./Primitives";

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
