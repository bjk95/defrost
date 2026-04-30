import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getTests, getTestRun } from "@/api";
import { decodeTestId, fmt, testStats } from "@/lib/utils";
import { useSuppressions } from "@/lib/suppressions";
import type { Cell, RunSummary } from "@/types";
import { Avatar, SectionLabel, StatusPill } from "@/components/Primitives";
import { Icon } from "@/components/Icons";
import { DurationChart, type ChartPoint } from "@/components/DurationChart";

export function TestDetailPage() {
  const [searchParams] = useSearchParams();
  const testId = searchParams.get("id") ?? "";
  const navigate = useNavigate();
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["tests"],
    queryFn: getTests,
  });

  if (isLoading) return <p style={{ color: "var(--fg-muted)", fontSize: 13 }}>loading…</p>;
  if (isError) {
    return (
      <p style={{ color: "var(--danger)", fontSize: 13 }}>
        failed: {(error as Error).message}
      </p>
    );
  }
  if (!data) return null;

  const test = data.tests.find((t) => t.test_id === testId);
  if (!test) return <div style={{ padding: 32 }}>Test not found.</div>;

  return (
    <TestDetailInner
      testId={testId}
      runs={data.runs}
      cells={test.cells}
      onBack={() => navigate("/")}
      onOpenRun={(rid) =>
        navigate(`/run?id=${encodeURIComponent(rid)}&from=${encodeURIComponent(testId)}`)
      }
    />
  );
}

function TestDetailInner({
  testId,
  runs,
  cells,
  onBack,
  onOpenRun,
}: {
  testId: string;
  runs: RunSummary[];
  cells: Cell[];
  onBack: () => void;
  onOpenRun: (rid: string) => void;
}) {
  const {
    has,
    add,
    remove,
    isMutating,
    isError: suppressionsError,
    error: suppressionsErrorObj,
  } = useSuppressions();
  // Don't trust `has(testId)` when the read failed — the empty fallback
  // would render a real suppression as "not suppressed" and offer the
  // wrong action.
  const isSuppressed = !suppressionsError && has(testId);

  const points: ChartPoint[] = useMemo(() => {
    const byRun = new Map(cells.map((c) => [c.run_id, c] as const));
    return [...runs]
      .reverse()
      .filter((r) => byRun.has(r.run_id))
      .map((r) => ({ run: r, cell: byRun.get(r.run_id)! }));
  }, [runs, cells]);

  const stats = useMemo(() => testStats(cells), [cells]);
  const [selIdx, setSelIdx] = useState(points.length - 1);
  const sel = points[selIdx];

  const decoded = decodeTestId(testId);
  const lastSep = Math.max(decoded.lastIndexOf("/"), decoded.lastIndexOf("."));
  const pkg = lastSep === -1 ? "" : decoded.slice(0, lastSep);
  const name = lastSep === -1 ? decoded : decoded.slice(lastSep + 1);

  const lastPoint = points[points.length - 1];

  return (
    <div style={{ paddingBottom: 64 }}>
      <button
        onClick={onBack}
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: 6,
          background: "transparent",
          border: "none",
          cursor: "pointer",
          color: "var(--fg-muted)",
          fontSize: 13,
          padding: 0,
          marginBottom: 24,
        }}
      >
        <Icon.ArrowLeft /> All tests
      </button>

      {pkg && (
        <div
          style={{
            marginBottom: 4,
            fontFamily: "var(--font-mono)",
            fontSize: 12,
            color: "var(--fg-muted)",
          }}
        >
          {pkg}
        </div>
      )}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 12,
          flexWrap: "wrap",
          marginBottom: isSuppressed ? 16 : 24,
        }}
      >
        <h1
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 22,
            fontWeight: 500,
            letterSpacing: "-0.02em",
            margin: 0,
            wordBreak: "break-all",
          }}
        >
          {name}
        </h1>
        <StatusPill status={stats.lastStatus} />
        {stats.fail > 0 &&
          stats.pass > 0 &&
          stats.failRate > 0.05 &&
          stats.failRate < 0.5 && <StatusPill status="flaky" />}
        {isSuppressed && <StatusPill status="suppressed" />}
        <div style={{ flex: 1 }} />
        {suppressionsError ? (
          <SuppressionLoadError message={suppressionsErrorObj?.message} />
        ) : (
          <SuppressionAction
            suppressed={isSuppressed}
            pending={isMutating}
            onAdd={() => add(testId)}
            onRemove={() => remove(testId)}
          />
        )}
      </div>

      {isSuppressed && (
        <SuppressionBanner pending={isMutating} onRemove={() => remove(testId)} />
      )}

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(6, 1fr)",
          border: "1px solid var(--border)",
          borderRadius: 10,
          overflow: "hidden",
          background: "var(--bg)",
          marginBottom: 24,
        }}
      >
        <Stat label="Runs" value={stats.total} sub={stats.skip ? `${stats.skip} skipped` : null} />
        <Stat
          label="Pass rate"
          value={stats.ran ? Math.round((stats.pass / stats.ran) * 100) + "%" : "—"}
          sub={`${stats.pass}/${stats.ran}`}
        />
        <Stat
          label="Failures"
          value={stats.fail}
          sub={(stats.failRate * 100).toFixed(1) + "%"}
          accent={stats.fail > 0 ? "danger" : null}
        />
        <Stat label="p50" value={fmt.duration(stats.p50)} mono />
        <Stat label="p95" value={fmt.duration(stats.p95)} mono />
        <Stat
          label="Last seen"
          value={lastPoint ? fmt.relTime(lastPoint.run.ts) : "—"}
          sub={lastPoint?.run.commit?.slice(0, 7)}
          mono
          noBorder
        />
      </div>

      <div
        style={{
          border: "1px solid var(--border)",
          borderRadius: 10,
          background: "var(--bg)",
          padding: 20,
          marginBottom: 24,
        }}
      >
        <SectionLabel right={<ChartLegend />}>Duration · {points.length} runs</SectionLabel>
        <DurationChart
          points={points}
          p50={stats.p50}
          p95={stats.p95}
          selIdx={selIdx}
          onSelect={setSelIdx}
          onOpen={(p) => onOpenRun(p.run.run_id)}
        />
      </div>

      {sel && (
        <SelectedRunCard
          point={sel}
          testId={testId}
          onOpenRun={() => onOpenRun(sel.run.run_id)}
        />
      )}

      <div style={{ marginTop: 32 }}>
        <SectionLabel>Run history</SectionLabel>
        <RunHistoryTable points={[...points].reverse()} onOpenRun={onOpenRun} />
      </div>
    </div>
  );
}

function Stat({
  label,
  value,
  sub,
  accent,
  mono,
  noBorder,
}: {
  label: string;
  value: number | string;
  sub?: string | null;
  accent?: "danger" | null;
  mono?: boolean;
  noBorder?: boolean;
}) {
  const color = accent === "danger" ? "var(--danger)" : "var(--fg)";
  return (
    <div
      style={{
        padding: "14px 16px",
        borderRight: noBorder ? "none" : "1px solid var(--border)",
      }}
    >
      <div
        style={{
          fontSize: 11,
          color: "var(--fg-muted)",
          letterSpacing: 0.06,
          textTransform: "uppercase",
          fontWeight: 500,
        }}
      >
        {label}
      </div>
      <div
        style={{
          fontSize: 22,
          fontWeight: 500,
          letterSpacing: "-0.02em",
          marginTop: 6,
          color,
          fontFamily: mono ? "var(--font-mono)" : "var(--font-sans)",
        }}
      >
        {value}
      </div>
      {sub && (
        <div
          style={{
            fontSize: 11,
            color: "var(--fg-muted)",
            marginTop: 4,
            fontFamily: "var(--font-mono)",
          }}
        >
          {sub}
        </div>
      )}
    </div>
  );
}

function ChartLegend() {
  return (
    <div
      style={{
        display: "flex",
        gap: 12,
        fontSize: 11,
        color: "var(--fg-muted)",
        fontFamily: "var(--font-mono)",
        textTransform: "none",
        letterSpacing: 0,
      }}
    >
      <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
        <span style={{ width: 8, height: 8, borderRadius: 2, background: "var(--success)" }} /> pass
      </span>
      <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
        <span style={{ width: 8, height: 8, borderRadius: 2, background: "var(--danger)" }} /> fail
      </span>
      <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
        <span style={{ width: 14, height: 1.5, background: "var(--accent)" }} /> p50
      </span>
      <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
        <svg width="14" height="3">
          <line x1="0" y1="1.5" x2="14" y2="1.5" stroke="var(--accent)" strokeDasharray="3 2" strokeWidth="1.5" />
        </svg>{" "}
        p95
      </span>
    </div>
  );
}

function SelectedRunCard({
  point,
  testId,
  onOpenRun,
}: {
  point: ChartPoint;
  testId: string;
  onOpenRun: () => void;
}) {
  const r = point.run;
  const c = point.cell;
  const detail = useQuery({
    queryKey: ["test", testId, "run", r.run_id],
    queryFn: () => getTestRun(testId, r.run_id),
    staleTime: Infinity,
  });

  return (
    <div style={{ marginTop: 24 }}>
      <SectionLabel
        right={
          <button
            onClick={onOpenRun}
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 4,
              fontSize: 12,
              background: "transparent",
              border: "none",
              color: "var(--accent)",
              cursor: "pointer",
            }}
          >
            Open run <Icon.ArrowUpRight />
          </button>
        }
      >
        Selected run · {r.commit?.slice(0, 7) ?? r.run_id}
      </SectionLabel>

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "minmax(0, 1fr) 320px",
          gap: 16,
          alignItems: "start",
        }}
      >
        <div
          style={{
            border: "1px solid var(--border)",
            borderRadius: 10,
            background: "var(--bg)",
            padding: 16,
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 12 }}>
            <StatusPill status={c.status} />
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 14, fontWeight: 500 }}>
              {fmt.duration(c.duration_ms)}
            </span>
            <span style={{ color: "var(--fg-muted)", fontSize: 12 }}>· {fmt.relTime(r.ts)}</span>
          </div>
          <pre
            style={{
              margin: 0,
              background: "var(--terminal-bg)",
              color: c.status === "fail" ? "oklch(0.78 0.18 27)" : "var(--terminal-fg)",
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              lineHeight: 1.55,
              padding: 12,
              borderRadius: 6,
              maxHeight: 220,
              overflow: "auto",
              whiteSpace: "pre-wrap",
            }}
          >
            {detail.isLoading
              ? "loading…"
              : detail.isError
                ? `failed: ${(detail.error as Error).message}`
                : detail.data?.test.output || "(no output)"}
          </pre>
        </div>
        <div
          style={{
            border: "1px solid var(--border)",
            borderRadius: 10,
            background: "var(--bg)",
            overflow: "hidden",
          }}
        >
          <RunMetaList run={r} totalMs={detail.data?.test.duration_ms} />
        </div>
      </div>
    </div>
  );
}

function RunMetaList({ run, totalMs }: { run: RunSummary; totalMs?: number }) {
  const rows: Array<[string, string, boolean]> = [
    ["commit", run.commit?.slice(0, 7) ?? "—", true],
    ["branch", run.branch ?? "—", true],
    ["author", run.author_name ?? run.author_email ?? "—", false],
    ...(run.pr ? ([["PR", "#" + run.pr, true]] as Array<[string, string, boolean]>) : []),
    ["os", run.os && run.arch ? `${run.os}/${run.arch}` : "—", true],
    ...(totalMs !== undefined
      ? ([["duration", fmt.durationShort(totalMs), true]] as Array<[string, string, boolean]>)
      : []),
    ["when", fmt.absTime(run.ts), false],
  ];
  return (
    <div>
      {rows.map(([k, v, mono], i) => (
        <div
          key={k}
          style={{
            display: "grid",
            gridTemplateColumns: "80px 1fr",
            gap: 10,
            padding: "9px 14px",
            borderBottom: i === rows.length - 1 ? "none" : "1px solid var(--border)",
            fontSize: 12,
          }}
        >
          <span
            style={{
              color: "var(--fg-muted)",
              textTransform: "uppercase",
              letterSpacing: 0.06,
              fontSize: 10,
              fontWeight: 500,
              alignSelf: "center",
            }}
          >
            {k}
          </span>
          <span
            style={{
              fontFamily: mono ? "var(--font-mono)" : "var(--font-sans)",
              color: "var(--fg)",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {v}
          </span>
        </div>
      ))}
    </div>
  );
}

function RunHistoryTable({
  points,
  onOpenRun,
}: {
  points: ChartPoint[];
  onOpenRun: (rid: string) => void;
}) {
  return (
    <div
      style={{
        border: "1px solid var(--border)",
        borderRadius: 10,
        overflow: "hidden",
        background: "var(--bg)",
      }}
    >
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "90px 90px 1fr 110px 110px 90px",
          gap: 10,
          alignItems: "center",
          padding: "10px 14px",
          background: "var(--bg-subtle)",
          borderBottom: "1px solid var(--border)",
          fontSize: 10,
          color: "var(--fg-muted)",
          letterSpacing: 0.06,
          textTransform: "uppercase",
          fontWeight: 500,
        }}
      >
        <span>Status</span>
        <span style={{ textAlign: "right" }}>Duration</span>
        <span>Commit</span>
        <span>Branch</span>
        <span>Author</span>
        <span style={{ textAlign: "right" }}>When</span>
      </div>
      {points.slice(0, 14).map(({ run, cell }) => (
        <div
          key={run.run_id}
          onClick={() => onOpenRun(run.run_id)}
          style={{
            display: "grid",
            gridTemplateColumns: "90px 90px 1fr 110px 110px 90px",
            gap: 10,
            alignItems: "center",
            padding: "10px 14px",
            borderBottom: "1px solid var(--border)",
            cursor: "pointer",
            fontSize: 13,
          }}
          onMouseEnter={(e) => (e.currentTarget.style.background = "var(--bg-subtle)")}
          onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
        >
          <StatusPill status={cell.status} size="xs" />
          <span
            style={{
              textAlign: "right",
              fontFamily: "var(--font-mono)",
              fontSize: 12,
            }}
          >
            {fmt.duration(cell.duration_ms)}
          </span>
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 12,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {run.commit?.slice(0, 7) ?? run.run_id.slice(0, 7)}
          </span>
          <span
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 5,
              color: "var(--fg-muted)",
            }}
          >
            <Icon.GitBranch />{" "}
            <span
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 12,
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {run.branch ?? "—"}
            </span>
          </span>
          <span
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 6,
              fontSize: 12,
              color: "var(--fg-muted)",
              minWidth: 0,
            }}
          >
            <Avatar name={run.author_name} size={18} />
            <span
              style={{
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {run.author_name?.split(" ")[0] ?? "—"}
            </span>
          </span>
          <span
            style={{
              textAlign: "right",
              color: "var(--fg-muted)",
              fontSize: 12,
              fontFamily: "var(--font-mono)",
            }}
          >
            {fmt.relTime(run.ts)}
          </span>
        </div>
      ))}
    </div>
  );
}

function SuppressionAction({
  suppressed,
  pending,
  onAdd,
  onRemove,
}: {
  suppressed: boolean;
  pending: boolean;
  onAdd: () => void;
  onRemove: () => void;
}) {
  const [hover, setHover] = useState(false);
  const [confirming, setConfirming] = useState(false);

  // Once the mutation settles and the test is suppressed, reset the
  // confirm flag so a later un-suppress + re-add starts from the default
  // Add button rather than landing back inside the confirm UI.
  useEffect(() => {
    if (suppressed) setConfirming(false);
  }, [suppressed]);

  if (suppressed) {
    return (
      <button
        onClick={onRemove}
        disabled={pending}
        onMouseEnter={() => setHover(true)}
        onMouseLeave={() => setHover(false)}
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: 6,
          padding: "6px 12px",
          fontSize: 12,
          fontWeight: 500,
          background: hover ? "var(--bg-subtle)" : "var(--bg)",
          color: "var(--fg)",
          border: "1px solid var(--border)",
          borderRadius: 6,
          cursor: pending ? "wait" : "pointer",
          opacity: pending ? 0.6 : 1,
        }}
      >
        <Icon.Check /> Remove from suppression list
      </button>
    );
  }
  if (confirming) {
    // Keep the confirm UI on screen — and both buttons disabled — while
    // the POST is in flight so a slow backend can't accept duplicate
    // clicks. The effect above resets `confirming` once the suppressed
    // state flips, so we never get stuck here.
    return (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
        <span style={{ fontSize: 12, color: "var(--fg-muted)" }}>
          {pending ? "Suppressing…" : "Suppress this test?"}
        </span>
        <button
          disabled={pending}
          onClick={onAdd}
          style={{
            padding: "6px 12px",
            fontSize: 12,
            fontWeight: 500,
            background: "var(--fg)",
            color: "var(--bg)",
            border: "1px solid var(--fg)",
            borderRadius: 6,
            cursor: pending ? "wait" : "pointer",
            opacity: pending ? 0.6 : 1,
          }}
        >
          Suppress
        </button>
        <button
          disabled={pending}
          onClick={() => setConfirming(false)}
          style={{
            padding: "6px 10px",
            fontSize: 12,
            background: "transparent",
            color: "var(--fg-muted)",
            border: "1px solid var(--border)",
            borderRadius: 6,
            cursor: pending ? "wait" : "pointer",
            opacity: pending ? 0.6 : 1,
          }}
        >
          Cancel
        </button>
      </span>
    );
  }
  return (
    <button
      onClick={() => setConfirming(true)}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        padding: "6px 12px",
        fontSize: 12,
        fontWeight: 500,
        background: hover ? "var(--bg-subtle)" : "var(--bg)",
        color: "var(--fg-muted)",
        border: "1px solid var(--border)",
        borderRadius: 6,
        cursor: "pointer",
      }}
    >
      <Icon.EyeOff /> Add to suppression list
    </button>
  );
}

function SuppressionLoadError({ message }: { message?: string }) {
  return (
    <span
      title={message || "Suppression state unavailable"}
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        padding: "6px 12px",
        fontSize: 12,
        color: "var(--danger)",
        border: "1px solid var(--border)",
        borderRadius: 6,
        background: "var(--bg)",
        cursor: "help",
      }}
    >
      <Icon.AlertTriangle /> Suppression state unavailable
    </span>
  );
}

function SuppressionBanner({
  pending,
  onRemove,
}: {
  pending: boolean;
  onRemove: () => void;
}) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 12,
        padding: "10px 14px",
        marginBottom: 24,
        background: "var(--bg-subtle)",
        border: "1px solid var(--border)",
        borderLeft: "3px solid var(--fg-muted)",
        borderRadius: 6,
        fontSize: 13,
      }}
    >
      <Icon.EyeOff />
      <span style={{ color: "var(--fg)" }}>
        Suppressed — failures here won't fail the build, but the test still runs.
      </span>
      <div style={{ flex: 1 }} />
      <button
        onClick={onRemove}
        disabled={pending}
        style={{
          fontSize: 12,
          background: "transparent",
          border: "none",
          cursor: pending ? "wait" : "pointer",
          color: "var(--accent)",
          padding: 0,
          fontWeight: 500,
          opacity: pending ? 0.6 : 1,
        }}
      >
        {pending ? "Removing…" : "Remove"}
      </button>
    </div>
  );
}
