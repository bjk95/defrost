import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getTests } from "@/api";
import {
  decodeTestId,
  fmt,
  testStats,
  useSuppressions,
  type TestStats,
} from "@/lib/utils";
import type { Cell, TestRow } from "@/types";
import { StatusPill } from "@/components/Primitives";
import { Icon } from "@/components/Icons";
import { SearchInput } from "@/components/Controls";

interface Row {
  test_id: string;
  test?: TestRow;
  stats?: TestStats;
  lastCell?: Cell;
}

export function SuppressionsPage() {
  const navigate = useNavigate();
  const suppressions = useSuppressions();
  const items = suppressions.ids;
  const { data } = useQuery({ queryKey: ["tests"], queryFn: getTests });
  const [q, setQ] = useState("");

  const testById = useMemo(() => {
    const m = new Map<string, TestRow>();
    for (const t of data?.tests ?? []) m.set(t.test_id, t);
    return m;
  }, [data]);

  // While a remove is in flight the optimistic update has already
  // filtered the row out of `items`. We re-add it for display so the
  // row stays visible (with its spinner) until the mutation settles —
  // otherwise the row vanishes on click and the loading state never
  // gets a chance to render.
  const displayedIds = useMemo(() => {
    const out = [...items];
    const pending = suppressions.pendingRemoveId;
    if (pending && !out.includes(pending)) out.push(pending);
    return out;
  }, [items, suppressions.pendingRemoveId]);

  const rows = useMemo<Row[]>(() => {
    const needle = q.trim().toLowerCase();
    return displayedIds
      .map((id) => {
        const test = testById.get(id);
        const stats = test ? testStats(test.cells) : undefined;
        const lastCell = test
          ? [...test.cells].reverse().find((c) => c.status !== "skip")
          : undefined;
        return { test_id: id, test, stats, lastCell };
      })
      .filter((r) => !needle || r.test_id.toLowerCase().includes(needle));
  }, [displayedIds, q, testById]);

  const onOpenTest = (testId: string) =>
    navigate(`/test?id=${encodeURIComponent(testId)}`);
  const onGoTests = () => navigate("/");

  return (
    <div style={{ paddingBottom: 64 }}>
      <div
        style={{
          marginBottom: 4,
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          color: "var(--fg-muted)",
        }}
      >
        suppressions.json
      </div>
      <div
        style={{
          display: "flex",
          alignItems: "baseline",
          gap: 12,
          flexWrap: "wrap",
          marginBottom: 6,
        }}
      >
        <h1
          style={{
            fontSize: 22,
            fontWeight: 500,
            letterSpacing: "-0.02em",
            margin: 0,
          }}
        >
          Suppression list
        </h1>
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 13,
            color: "var(--fg-muted)",
          }}
        >
          {displayedIds.length} {displayedIds.length === 1 ? "test" : "tests"}
        </span>
      </div>
      <p
        style={{
          margin: "0 0 24px",
          color: "var(--fg-muted)",
          fontSize: 13,
          maxWidth: 640,
        }}
      >
        Suppressed tests still run on every commit, but failures don't break the build. Use this as a
        temporary escape hatch for known-bad tests — not as a permanent home.
      </p>

      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 12,
          marginBottom: 16,
          flexWrap: "wrap",
        }}
      >
        <SearchInput value={q} onChange={setQ} placeholder="Filter suppressed tests…" />
        <div style={{ flex: 1 }} />
        <button
          onClick={onGoTests}
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: 6,
            padding: "6px 12px",
            fontSize: 12,
            fontWeight: 500,
            background: "var(--bg)",
            color: "var(--fg-muted)",
            border: "1px solid var(--border)",
            borderRadius: 6,
            cursor: "pointer",
          }}
        >
          <Icon.Plus /> Suppress a test
        </button>
      </div>

      <div
        style={{
          border: "1px solid var(--border)",
          borderRadius: 10,
          background: "var(--bg)",
          overflow: "hidden",
        }}
      >
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "minmax(0,1fr) 90px 90px 80px 36px",
            gap: 12,
            alignItems: "center",
            padding: "10px 16px",
            borderBottom: "1px solid var(--border)",
            background: "var(--bg-subtle)",
            fontSize: 11,
            color: "var(--fg-muted)",
            textTransform: "uppercase",
            letterSpacing: 0.06,
            fontWeight: 500,
          }}
        >
          <span>Test</span>
          <span style={{ textAlign: "right" }}>Last</span>
          <span style={{ textAlign: "right" }}>Fail rate</span>
          <span style={{ textAlign: "right" }}>p50</span>
          <span></span>
        </div>

        {suppressions.isLoading ? (
          <LoadingState />
        ) : suppressions.isError ? (
          <ErrorState message={suppressions.error?.message ?? "Failed to load suppressions."} />
        ) : rows.length === 0 ? (
          <EmptyState
            hasFilter={!!q.trim()}
            onGoTests={onGoTests}
            onClearFilter={() => setQ("")}
          />
        ) : (
          rows.map((r) => (
            <SuppressionRow
              key={r.test_id}
              row={r}
              removing={suppressions.pendingRemoveId === r.test_id}
              onOpen={() => onOpenTest(r.test_id)}
              onRemove={() => suppressions.remove(r.test_id)}
            />
          ))
        )}
      </div>

      {rows.length > 0 && (
        <div
          style={{
            marginTop: 16,
            fontSize: 12,
            color: "var(--fg-muted)",
            display: "flex",
            alignItems: "center",
            gap: 8,
          }}
        >
          <Icon.AlertTriangle />
          <span>
            {rows.filter((r) => r.lastCell?.status === "fail").length} of {rows.length} have failed in
            their most recent run.
          </span>
        </div>
      )}
    </div>
  );
}

function SuppressionRow({
  row,
  removing,
  onOpen,
  onRemove,
}: {
  row: Row;
  removing: boolean;
  onOpen: () => void;
  onRemove: () => void;
}) {
  const [hover, setHover] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const stats = row.stats;
  const last = row.lastCell;
  const lastStatus = last?.status ?? "skip";

  const decoded = decodeTestId(row.test_id);
  const dot = Math.max(decoded.lastIndexOf("/"), decoded.lastIndexOf("."));
  const pkg = dot === -1 ? "" : decoded.slice(0, dot);
  const name = dot === -1 ? decoded : decoded.slice(dot + 1);

  return (
    <div
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => {
        setHover(false);
        setConfirming(false);
      }}
      onClick={removing ? undefined : onOpen}
      style={{
        display: "grid",
        gridTemplateColumns: "minmax(0,1fr) 90px 90px 80px 36px",
        gap: 12,
        alignItems: "center",
        padding: "10px 16px",
        borderBottom: "1px solid var(--border)",
        background: hover && !removing ? "var(--bg-subtle)" : "transparent",
        transition: "background var(--dur-fast) var(--ease-out)",
        cursor: removing ? "wait" : "pointer",
        opacity: removing ? 0.55 : 1,
      }}
    >
      <div style={{ minWidth: 0, display: "flex", flexDirection: "column", gap: 2 }}>
        {pkg && (
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: "var(--fg-muted)",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {pkg}
          </span>
        )}
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 13,
            color: "var(--fg)",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {name}
        </span>
      </div>

      <div style={{ display: "flex", justifyContent: "flex-end" }}>
        <StatusPill status={lastStatus} size="xs" />
      </div>

      <span
        style={{
          textAlign: "right",
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          color: stats && stats.fail > 0 ? "var(--danger)" : "var(--fg-muted)",
        }}
      >
        {stats ? (stats.failRate * 100).toFixed(1) + "%" : "—"}
      </span>

      <span
        style={{
          textAlign: "right",
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          color: "var(--fg-muted)",
        }}
      >
        {stats ? fmt.duration(stats.p50) : "—"}
      </span>

      <div
        style={{ display: "flex", justifyContent: "flex-end" }}
        onClick={(e) => e.stopPropagation()}
      >
        {removing ? (
          <span
            title="Removing…"
            style={{
              padding: 6,
              display: "inline-flex",
              alignItems: "center",
              color: "var(--fg-muted)",
            }}
          >
            <Spinner />
          </span>
        ) : confirming ? (
          <button
            onClick={onRemove}
            title="Confirm remove"
            style={{
              fontSize: 11,
              fontWeight: 500,
              padding: "4px 8px",
              background: "var(--danger)",
              color: "white",
              border: "none",
              borderRadius: 4,
              cursor: "pointer",
            }}
          >
            Remove
          </button>
        ) : (
          <button
            onClick={() => setConfirming(true)}
            title="Remove from suppression list"
            style={{
              opacity: hover ? 1 : 0.55,
              padding: 6,
              background: "transparent",
              border: "1px solid var(--border)",
              borderRadius: 4,
              color: "var(--fg-muted)",
              cursor: "pointer",
              display: "inline-flex",
              alignItems: "center",
              transition: "opacity var(--dur-fast) var(--ease-out)",
            }}
          >
            <Icon.X />
          </button>
        )}
      </div>
    </div>
  );
}

function Spinner() {
  return (
    <span
      aria-hidden
      style={{
        display: "inline-block",
        width: 12,
        height: 12,
        border: "1.5px solid currentColor",
        borderTopColor: "transparent",
        borderRadius: "50%",
        animation: "defrostSuppressSpin 0.8s linear infinite",
      }}
    >
      <style>{`@keyframes defrostSuppressSpin { to { transform: rotate(360deg); } }`}</style>
    </span>
  );
}

function LoadingState() {
  return (
    <div
      style={{
        padding: "48px 24px",
        textAlign: "center",
        color: "var(--fg-muted)",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        gap: 12,
      }}
    >
      <span style={{ color: "var(--fg-muted)" }}>
        <Spinner />
      </span>
      <div style={{ fontSize: 13 }}>Loading suppressions…</div>
      <div style={{ fontSize: 11, color: "var(--fg-subtle)" }}>
        cloning data branch
      </div>
    </div>
  );
}

function ErrorState({ message }: { message: string }) {
  return (
    <div
      style={{
        padding: "48px 24px",
        textAlign: "center",
        color: "var(--fg-muted)",
      }}
    >
      <div style={{ fontSize: 13, color: "var(--danger)", marginBottom: 4 }}>
        Failed to load suppressions
      </div>
      <div style={{ fontSize: 12, fontFamily: "var(--font-mono)" }}>{message}</div>
    </div>
  );
}

function EmptyState({
  hasFilter,
  onGoTests,
  onClearFilter,
}: {
  hasFilter: boolean;
  onGoTests: () => void;
  onClearFilter: () => void;
}) {
  return (
    <div
      style={{
        padding: "48px 24px",
        textAlign: "center",
        color: "var(--fg-muted)",
      }}
    >
      <div
        style={{
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          width: 40,
          height: 40,
          borderRadius: 999,
          background: "var(--bg-subtle)",
          marginBottom: 12,
          color: "var(--fg-subtle)",
        }}
      >
        <Icon.EyeOff />
      </div>
      <div style={{ fontSize: 14, color: "var(--fg)", marginBottom: 4 }}>
        {hasFilter ? "No suppressed tests match that filter." : "No tests are suppressed."}
      </div>
      <div style={{ fontSize: 12, marginBottom: 16 }}>
        {hasFilter
          ? "Try a shorter query."
          : "Suppress a flaky test from its detail page to give the team breathing room while you fix the root cause."}
      </div>
      <button
        onClick={hasFilter ? onClearFilter : onGoTests}
        style={{
          padding: "6px 12px",
          fontSize: 12,
          fontWeight: 500,
          background: "var(--bg)",
          color: "var(--fg)",
          border: "1px solid var(--border)",
          borderRadius: 6,
          cursor: "pointer",
        }}
      >
        {hasFilter ? "Clear filter" : "Browse tests"}
      </button>
    </div>
  );
}
