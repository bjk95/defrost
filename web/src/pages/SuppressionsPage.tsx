import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import Fuse from "fuse.js";
import { getTests } from "@/api";
import {
  decodeTestId,
  fmt,
  testStats,
  type TestStats,
} from "@/lib/utils";
import { useSuppressions, type SuppressionEntry } from "@/lib/suppressions";
import type { Cell, TestRow } from "@/types";
import { Avatar, StatusPill } from "@/components/Primitives";
import { Icon } from "@/components/Icons";
import { SearchInput } from "@/components/Controls";

interface Row extends SuppressionEntry {
  test?: TestRow;
  stats?: TestStats;
  lastCell?: Cell;
}

export function SuppressionsPage() {
  const navigate = useNavigate();
  const {
    entries: items,
    remove,
    isLoading,
    isError,
    error,
    isMutating,
  } = useSuppressions();
  const { data } = useQuery({ queryKey: ["tests"], queryFn: getTests });
  const [q, setQ] = useState("");

  const testById = useMemo(() => {
    const m = new Map<string, TestRow>();
    for (const t of data?.tests ?? []) m.set(t.test_id, t);
    return m;
  }, [data]);

  const allRows = useMemo<Row[]>(
    () =>
      items.map((entry) => {
        const test = testById.get(entry.test_id);
        const stats = test ? testStats(test.cells) : undefined;
        const lastCell = test
          ? [...test.cells].reverse().find((c) => c.status !== "skip")
          : undefined;
        return { ...entry, test, stats, lastCell };
      }),
    [items, testById],
  );

  // Fuse indexes the decoded form so users can type the package + test
  // name as they read it on screen (slashes, dots, mixed case all work),
  // not the URL-encoded test_id we store on disk.
  const fuse = useMemo(
    () =>
      new Fuse(
        allRows.map((r) => ({ row: r, decoded: decodeTestId(r.test_id) })),
        {
          keys: ["decoded"],
          threshold: 0.4,
          ignoreLocation: true,
          minMatchCharLength: 2,
        },
      ),
    [allRows],
  );

  const rows = useMemo<Row[]>(() => {
    const needle = q.trim();
    if (!needle) return allRows;
    return fuse.search(needle).map((res) => res.item.row);
  }, [allRows, fuse, q]);

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
          {isLoading
            ? "loading…"
            : isError
              ? "unavailable"
              : `${items.length} ${items.length === 1 ? "test" : "tests"}`}
        </span>
        {isMutating && (
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 12,
              color: "var(--accent)",
            }}
          >
            saving…
          </span>
        )}
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
            gridTemplateColumns: "minmax(0,1fr) 90px 90px 80px 130px 150px 36px",
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
          <span>Suppressed</span>
          <span>By</span>
          <span></span>
        </div>

        {isLoading ? (
          <LoadingState />
        ) : isError ? (
          <ErrorState message={error?.message} />
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
              pending={isMutating}
              onOpen={() => onOpenTest(r.test_id)}
              onRemove={() => remove(r.test_id)}
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
  pending,
  onOpen,
  onRemove,
}: {
  row: Row;
  pending: boolean;
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

  const relAdded = "—";
  const absAdded = "";
  const byLabel = "you";

  return (
    <div
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => {
        setHover(false);
        setConfirming(false);
      }}
      onClick={() => row.test && onOpen()}
      style={{
        display: "grid",
        gridTemplateColumns: "minmax(0,1fr) 90px 90px 80px 130px 150px 36px",
        gap: 12,
        alignItems: "center",
        padding: "10px 16px",
        borderBottom: "1px solid var(--border)",
        background: hover ? "var(--bg-subtle)" : "transparent",
        transition: "background var(--dur-fast) var(--ease-out)",
        cursor: row.test ? "pointer" : "default",
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

      <div style={{ display: "flex", flexDirection: "column", minWidth: 0 }}>
        <span style={{ fontSize: 12, color: "var(--fg)" }}>{relAdded}</span>
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 10.5,
            color: "var(--fg-subtle)",
          }}
        >
          {absAdded}
        </span>
      </div>

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
        <Avatar name={byLabel} size={18} />
        <span
          style={{
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {byLabel}
        </span>
      </span>

      <div
        style={{ display: "flex", justifyContent: "flex-end" }}
        onClick={(e) => e.stopPropagation()}
      >
        {confirming ? (
          <button
            onClick={onRemove}
            disabled={pending}
            title="Confirm remove"
            style={{
              fontSize: 11,
              fontWeight: 500,
              padding: "4px 8px",
              background: "var(--danger)",
              color: "white",
              border: "none",
              borderRadius: 4,
              cursor: pending ? "wait" : "pointer",
              opacity: pending ? 0.6 : 1,
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

function ErrorState({ message }: { message?: string }) {
  return (
    <div
      style={{
        padding: "48px 24px",
        textAlign: "center",
        color: "var(--fg-muted)",
        fontSize: 13,
      }}
    >
      <div style={{ color: "var(--danger)", marginBottom: 4, fontWeight: 500 }}>
        Could not load suppressions.
      </div>
      <div style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>
        {message || "Try refreshing the page."}
      </div>
    </div>
  );
}

function LoadingState() {
  return (
    <div
      style={{
        padding: "48px 24px",
        textAlign: "center",
        color: "var(--fg-muted)",
        fontSize: 13,
      }}
    >
      Loading suppressions…
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
