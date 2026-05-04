import type { CSSProperties, ReactNode } from "react";
import type { Cell, RunSummary, TestRow } from "@/types";
import { fmt, cn } from "@/lib/utils";
import { Card as UICard } from "@/components/ui/card";

export const CELL_PASS = "var(--success)";
export const CELL_FAIL = "var(--danger)";
export const CELL_SKIP = "var(--gray-300)";

export function statusColor(status?: string): string {
  if (status === "pass") return CELL_PASS;
  if (status === "fail") return CELL_FAIL;
  if (status === "skip") return CELL_SKIP;
  return "transparent";
}

export function StatusPill({
  status,
  size = "sm",
}: {
  status: string;
  size?: "xs" | "sm";
}) {
  const pad = size === "xs" ? "1px 6px" : "2px 8px";
  const fs = size === "xs" ? 10 : 11;
  const cfg = (
    {
      pass: { bg: "color-mix(in oklch, var(--success) 14%, transparent)", fg: "var(--success)", label: "pass" },
      fail: { bg: "color-mix(in oklch, var(--danger) 14%, transparent)", fg: "var(--danger)", label: "fail" },
      skip: { bg: "var(--bg-muted)", fg: "var(--fg-muted)", label: "skip" },
      flaky: {
        bg: "color-mix(in oklch, var(--warning) 16%, transparent)",
        fg: "color-mix(in oklch, var(--warning) 75%, var(--fg))",
        label: "flaky",
      },
      running: {
        bg: "color-mix(in oklch, var(--accent) 14%, transparent)",
        fg: "var(--accent)",
        label: "running",
      },
      suppressed: { bg: "var(--bg-muted)", fg: "var(--fg-muted)", label: "suppressed" },
    } as Record<string, { bg: string; fg: string; label: string }>
  )[status] ?? { bg: "var(--bg-muted)", fg: "var(--fg-muted)", label: status };

  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 5,
        padding: pad,
        fontSize: fs,
        fontWeight: 500,
        lineHeight: 1,
        background: cfg.bg,
        color: cfg.fg,
        borderRadius: 999,
        fontFamily: "var(--font-mono)",
        whiteSpace: "nowrap",
      }}
    >
      <span style={{ width: 5, height: 5, borderRadius: 999, background: "currentColor" }} />
      {cfg.label}
    </span>
  );
}

export function RunCell({
  status,
  onClick,
  selected,
  title,
  size = 12,
}: {
  status?: string;
  onClick?: () => void;
  selected?: boolean;
  title?: string;
  size?: number;
}) {
  const isNull = !status;
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      disabled={isNull}
      data-testid={status ? `run-cell-${status}` : "run-cell-empty"}
      style={{
        width: size,
        height: size,
        padding: 0,
        background: isNull ? "transparent" : statusColor(status),
        opacity: status === "skip" ? 0.6 : 1,
        border: isNull ? "1px dashed var(--border)" : "none",
        borderRadius: 2,
        cursor: isNull ? "default" : "pointer",
        outline: selected ? "2px solid var(--accent)" : "none",
        outlineOffset: selected ? 1 : 0,
        transition:
          "transform var(--dur-fast) var(--ease-out), outline-offset var(--dur-fast) var(--ease-out)",
        display: "block",
      }}
      onMouseEnter={(e) => {
        if (!isNull) (e.currentTarget as HTMLButtonElement).style.transform = "scale(1.4)";
      }}
      onMouseLeave={(e) => {
        (e.currentTarget as HTMLButtonElement).style.transform = "scale(1)";
      }}
    />
  );
}

export function CountsBar({
  counts,
  total,
  width = 96,
  height = 6,
}: {
  counts: { pass: number; fail: number; skip: number };
  total?: number;
  width?: number;
  height?: number;
}) {
  const sum = total || counts.pass + counts.fail + counts.skip || 1;
  const seg = (v: number, color: string) =>
    v ? <div style={{ width: (v / sum) * 100 + "%", height: "100%", background: color }} /> : null;
  return (
    <div
      style={{
        display: "inline-flex",
        width,
        height,
        borderRadius: 999,
        overflow: "hidden",
        background: "var(--bg-muted)",
        border: "1px solid var(--border)",
      }}
    >
      {seg(counts.pass, "var(--success)")}
      {seg(counts.fail, "var(--danger)")}
      {seg(counts.skip, "var(--gray-400)")}
    </div>
  );
}

export function MetaPill({
  icon,
  label,
  value,
  mono,
}: {
  icon?: ReactNode;
  label?: string;
  value: ReactNode;
  mono?: boolean;
}) {
  return (
    <div
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 8,
        padding: "5px 10px",
        fontSize: 12,
        lineHeight: 1.2,
        background: "var(--bg-subtle)",
        border: "1px solid var(--border)",
        borderRadius: 6,
        color: "var(--fg)",
        fontFamily: mono ? "var(--font-mono)" : "var(--font-sans)",
      }}
    >
      {icon && (
        <span style={{ color: "var(--fg-muted)", display: "inline-flex" }}>{icon}</span>
      )}
      {label && (
        <span style={{ color: "var(--fg-muted)", fontFamily: "var(--font-sans)" }}>{label}</span>
      )}
      <span style={{ color: "var(--fg)", fontWeight: 500 }}>{value}</span>
    </div>
  );
}

export function Avatar({ name, size = 20 }: { name?: string; size?: number }) {
  return (
    <div
      style={{
        width: size,
        height: size,
        borderRadius: 999,
        background: "var(--bg-muted)",
        border: "1px solid var(--border)",
        color: "var(--fg-muted)",
        fontSize: size <= 20 ? 10 : 11,
        fontWeight: 500,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        fontFamily: "var(--font-mono)",
      }}
    >
      {fmt.initials(name)}
    </div>
  );
}

export function SectionLabel({
  children,
  right,
}: {
  children: ReactNode;
  right?: ReactNode;
}) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        fontSize: 11,
        fontWeight: 500,
        color: "var(--fg-muted)",
        letterSpacing: 0.08,
        textTransform: "uppercase",
        marginBottom: 10,
      }}
    >
      <span>{children}</span>
      {right}
    </div>
  );
}

// Card wraps the shadcn Card primitive with the legacy props (padding as
// number, custom hover, onClick). Visuals match the previous hand-rolled
// version — same border, radius, and bg-subtle hover swap.
export function Card({
  children,
  padding = 16,
  style,
  onClick,
  hover,
  className,
}: {
  children: ReactNode;
  padding?: number;
  style?: CSSProperties;
  onClick?: () => void;
  hover?: boolean;
  className?: string;
}) {
  return (
    <UICard
      onClick={onClick}
      className={cn(
        "rounded-[10px] shadow-none transition-colors",
        hover && "hover:bg-bg-subtle",
        onClick && "cursor-pointer",
        className,
      )}
      style={{ padding, ...style }}
    >
      {children}
    </UICard>
  );
}

// Per-test history strip — renders one square per visible run, oldest→newest left→right.
export function HistoryStrip({
  row,
  runs,
  onCellClick,
  selectedRunId,
  cellSize = 11,
  gap = 3,
}: {
  row: TestRow;
  runs: RunSummary[];
  onCellClick?: (run: RunSummary, cell: Cell) => void;
  selectedRunId?: string;
  cellSize?: number;
  gap?: number;
}) {
  const ordered = [...runs].reverse();
  const byRun = new Map(row.cells.map((c) => [c.run_id, c] as const));
  return (
    <div style={{ display: "flex", gap, alignItems: "center" }}>
      {ordered.map((run) => {
        const cell = byRun.get(run.run_id);
        return (
          <RunCell
            key={run.run_id}
            status={cell?.status}
            onClick={cell && onCellClick ? () => onCellClick(run, cell) : undefined}
            selected={selectedRunId === run.run_id}
            title={
              cell
                ? `${cell.status} · ${fmt.duration(cell.duration_ms)} · ${run.commit?.slice(0, 7) ?? ""} · ${fmt.relTime(run.ts)}`
                : `not in run ${run.commit?.slice(0, 7) ?? ""}`
            }
            size={cellSize}
          />
        );
      })}
    </div>
  );
}

// Group strip — one square per run, color = worst status across the group's tests.
export function GroupHistoryStrip({
  runs,
  cells,
  compact,
}: {
  runs: RunSummary[];
  cells: Cell[];
  compact?: boolean;
}) {
  const byRun = new Map<string, string>();
  for (const c of cells) {
    const cur = byRun.get(c.run_id);
    let next = cur;
    if (!cur) next = c.status;
    else if (cur === "fail" || c.status === "fail") next = "fail";
    else if (cur === "pass" || c.status === "pass") next = "pass";
    else next = "skip";
    byRun.set(c.run_id, next!);
  }
  const ordered = [...runs].reverse();
  return (
    <div style={{ display: "flex", gap: 3 }}>
      {ordered.map((r) => {
        const status = byRun.get(r.run_id);
        return (
          <div
            key={r.run_id}
            style={{
              width: compact ? 9 : 11,
              height: compact ? 9 : 11,
              borderRadius: 2,
              background: status ? statusColor(status) : "transparent",
              border: status ? "none" : "1px dashed var(--border)",
              opacity: status === "skip" ? 0.6 : 1,
            }}
          />
        );
      })}
    </div>
  );
}
