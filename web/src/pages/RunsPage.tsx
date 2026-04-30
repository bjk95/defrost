import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getTests } from "@/api";
import { fmt, runCounts as computeRunCounts } from "@/lib/utils";
import type { RunSummary, TestRow } from "@/types";
import { Avatar } from "@/components/Primitives";
import { Icon } from "@/components/Icons";
import { SearchInput, Segmented } from "@/components/Controls";
import { RunsEmpty } from "@/components/EmptyStates";

type RunFilter = "all" | "fail" | "pass";

export function RunsPage() {
  const navigate = useNavigate();
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["tests"],
    queryFn: getTests,
  });

  const [q, setQ] = useState("");
  const [status, setStatus] = useState<RunFilter>("all");

  const runs = data?.runs ?? [];
  const tests = data?.tests ?? [];

  const enriched = useMemo(
    () =>
      runs.map((r) => ({ run: r, counts: computeRunCounts(tests, r.run_id) })),
    [runs, tests],
  );

  const filtered = useMemo(
    () =>
      enriched.filter(({ run, counts }) => {
        if (q) {
          const hay = `${run.commit ?? ""} ${run.branch ?? ""} ${run.author_name ?? ""} ${run.author_email ?? ""}`.toLowerCase();
          if (!hay.includes(q.toLowerCase())) return false;
        }
        if (status === "fail" && counts.fail === 0) return false;
        if (status === "pass" && counts.fail > 0) return false;
        return true;
      }),
    [enriched, q, status],
  );

  if (isLoading) return <p style={{ color: "var(--fg-muted)", fontSize: 13 }}>loading…</p>;
  if (isError) {
    return (
      <p style={{ color: "var(--danger)", fontSize: 13 }}>
        failed: {(error as Error).message}
      </p>
    );
  }
  if (runs.length === 0) return <RunsEmpty />;

  return (
    <div>
      <div
        style={{
          display: "flex",
          gap: 12,
          alignItems: "center",
          paddingBottom: 16,
          marginBottom: 16,
          borderBottom: "1px solid var(--border)",
        }}
      >
        <SearchInput value={q} onChange={setQ} placeholder="Filter by commit, branch, author…" />
        <Segmented<RunFilter>
          value={status}
          onChange={setStatus}
          options={[
            { value: "all", label: "All" },
            { value: "fail", label: "Failing" },
            { value: "pass", label: "Passing" },
          ]}
        />
        <div style={{ flex: 1 }} />
        <span style={{ fontSize: 12, color: "var(--fg-muted)", fontFamily: "var(--font-mono)" }}>
          {filtered.length} of {runs.length} runs
        </span>
      </div>

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
            gridTemplateColumns: "100px 110px 110px 1fr 110px 110px",
            gap: 12,
            alignItems: "center",
            padding: "10px 16px",
            fontSize: 10,
            fontWeight: 500,
            color: "var(--fg-muted)",
            letterSpacing: 0.06,
            textTransform: "uppercase",
            background: "var(--bg-subtle)",
            borderBottom: "1px solid var(--border)",
          }}
        >
          <span>Commit</span>
          <span style={{ textAlign: "right" }}>Result</span>
          <span>Branch</span>
          <span>Author</span>
          <span style={{ textAlign: "right" }}>Duration</span>
          <span style={{ textAlign: "right" }}>When</span>
        </div>
        {filtered.map(({ run, counts }) => (
          <RunRow
            key={run.run_id}
            run={run}
            tests={tests}
            counts={counts}
            onClick={() => navigate(`/run?id=${encodeURIComponent(run.run_id)}`)}
          />
        ))}
        {filtered.length === 0 && (
          <div
            style={{
              padding: "32px 16px",
              textAlign: "center",
              color: "var(--fg-muted)",
              fontSize: 13,
            }}
          >
            No runs match these filters.
          </div>
        )}
      </div>
    </div>
  );
}

function RunRow({
  run,
  counts,
  onClick,
}: {
  run: RunSummary;
  tests: TestRow[];
  counts: { pass: number; fail: number; skip: number; total: number; total_ms: number };
  onClick: () => void;
}) {
  const total = counts.total;
  return (
    <div
      onClick={onClick}
      style={{
        display: "grid",
        gridTemplateColumns: "100px 110px 110px 1fr 110px 110px",
        gap: 12,
        alignItems: "center",
        padding: "12px 16px",
        borderBottom: "1px solid var(--border)",
        cursor: "pointer",
        fontSize: 13,
      }}
      onMouseEnter={(e) => (e.currentTarget.style.background = "var(--bg-subtle)")}
      onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
    >
      <span style={{ display: "flex", alignItems: "center", gap: 6 }}>
        <span
          style={{
            width: 6,
            height: 6,
            borderRadius: 999,
            background: counts.fail > 0 ? "var(--danger)" : "var(--success)",
          }}
        />
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>
          {run.commit?.slice(0, 7) ?? run.run_id.slice(0, 7)}
        </span>
      </span>
      <span
        style={{
          textAlign: "right",
          display: "flex",
          justifyContent: "flex-end",
          alignItems: "center",
          gap: 6,
          fontFamily: "var(--font-mono)",
          fontSize: 12,
        }}
      >
        {counts.fail > 0 ? (
          <span style={{ color: "var(--danger)" }}>{counts.fail} fail</span>
        ) : (
          <span style={{ color: "var(--success)" }}>all pass</span>
        )}
        <span style={{ color: "var(--fg-muted)" }}>· {total}</span>
      </span>
      <span
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: 5,
          color: "var(--fg-muted)",
          minWidth: 0,
        }}
      >
        <Icon.GitBranch />
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
          {run.author_name ?? run.author_email ?? "—"}
        </span>
        {run.pr ? (
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-subtle)" }}>
            #{run.pr}
          </span>
        ) : null}
      </span>
      <span
        style={{
          textAlign: "right",
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          color: "var(--fg-muted)",
        }}
      >
        {fmt.durationShort(counts.total_ms)}
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
  );
}
