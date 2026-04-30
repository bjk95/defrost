import { useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getTests } from "@/api";
import { decodeTestId, fmt, runCounts as computeRunCounts } from "@/lib/utils";
import type { RunSummary } from "@/types";
import { MetaPill, SectionLabel, StatusPill } from "@/components/Primitives";
import { Icon } from "@/components/Icons";
import { Segmented } from "@/components/Controls";

type RunFilter = "all" | "fail" | "pass" | "skip";

export function RunDetailPage() {
  const [params] = useSearchParams();
  const runId = params.get("id") ?? "";
  const fromTestId = params.get("from");
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

  const run = data.runs.find((r) => r.run_id === runId);
  if (!run) return <div style={{ padding: 32 }}>Run not found.</div>;

  return (
    <RunDetailInner
      run={run}
      tests={data.tests}
      onBack={() =>
        fromTestId ? navigate(`/test?id=${encodeURIComponent(fromTestId)}`) : navigate("/runs")
      }
      onOpenTest={(tid) => navigate(`/test?id=${encodeURIComponent(tid)}`)}
    />
  );
}

function RunDetailInner({
  run,
  tests,
  onBack,
  onOpenTest,
}: {
  run: RunSummary;
  tests: { test_id: string; test_name: string; cells: { run_id: string; status: string; duration_ms: number }[] }[];
  onBack: () => void;
  onOpenTest: (tid: string) => void;
}) {
  const [filter, setFilter] = useState<RunFilter>("all");

  const inRun = useMemo(() => {
    const out: Array<{ test: typeof tests[number]; cell: typeof tests[number]["cells"][number] }> = [];
    for (const t of tests) {
      const c = t.cells.find((c) => c.run_id === run.run_id);
      if (c) out.push({ test: t, cell: c });
    }
    out.sort((a, b) => {
      const r: Record<string, number> = { fail: 0, pass: 1, skip: 2 };
      const ra = r[a.cell.status] ?? 3;
      const rb = r[b.cell.status] ?? 3;
      if (ra !== rb) return ra - rb;
      return b.cell.duration_ms - a.cell.duration_ms;
    });
    return out;
  }, [run.run_id, tests]);

  const counts = useMemo(() => computeRunCounts(tests, run.run_id), [tests, run.run_id]);
  const visible = inRun.filter(({ cell }) => filter === "all" || cell.status === filter);

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
        <Icon.ArrowLeft /> Back
      </button>

      <div style={{ marginBottom: 24 }}>
        <div
          style={{
            fontSize: 12,
            color: "var(--fg-muted)",
            fontFamily: "var(--font-mono)",
            marginBottom: 4,
          }}
        >
          Run · {fmt.absTime(run.ts)}
        </div>
        <h1
          style={{
            fontSize: 22,
            fontWeight: 500,
            letterSpacing: "-0.02em",
            margin: 0,
            marginBottom: 10,
            fontFamily: "var(--font-mono)",
          }}
        >
          {run.commit?.slice(0, 7) ?? run.run_id.slice(0, 12)}
        </h1>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {run.commit && <MetaPill icon={<Icon.GitCommit />} value={run.commit.slice(0, 7)} mono />}
          {run.branch && <MetaPill icon={<Icon.GitBranch />} value={run.branch} mono />}
          {run.pr ? <MetaPill icon={<Icon.GitPullRequest />} value={"#" + run.pr} mono /> : null}
          {run.author_name && <MetaPill icon={<Icon.User />} value={run.author_name} />}
          {run.os && run.arch && (
            <MetaPill icon={<Icon.Cpu />} value={`${run.os}/${run.arch}`} mono />
          )}
          <MetaPill icon={<Icon.Clock />} value={fmt.durationShort(counts.total_ms)} mono />
        </div>
      </div>

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(4, 1fr)",
          border: "1px solid var(--border)",
          borderRadius: 10,
          overflow: "hidden",
          background: "var(--bg)",
          marginBottom: 24,
        }}
      >
        <RunStat label="Total" value={counts.total} />
        <RunStat label="Passed" value={counts.pass} accent="success" />
        <RunStat
          label="Failed"
          value={counts.fail}
          accent={counts.fail ? "danger" : null}
        />
        <RunStat label="Skipped" value={counts.skip} muted noBorder />
      </div>

      {run.cmd && run.cmd.length > 0 && (
        <div style={{ marginBottom: 24 }}>
          <SectionLabel>Command</SectionLabel>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
              padding: "10px 12px",
              background: "var(--terminal-bg)",
              color: "var(--terminal-fg)",
              borderRadius: 8,
              fontFamily: "var(--font-mono)",
              fontSize: 12,
            }}
          >
            <Icon.Terminal />
            <span style={{ flex: 1 }}>{run.cmd.join(" ")}</span>
          </div>
        </div>
      )}

      <SectionLabel
        right={
          <Segmented<RunFilter>
            value={filter}
            onChange={setFilter}
            options={[
              { value: "all", label: `All ${inRun.length}` },
              { value: "fail", label: `Failed ${counts.fail}` },
              { value: "pass", label: `Passed ${counts.pass}` },
              { value: "skip", label: `Skipped ${counts.skip}` },
            ]}
          />
        }
      >
        Tests in this run
      </SectionLabel>

      <div
        style={{
          border: "1px solid var(--border)",
          borderRadius: 10,
          overflow: "hidden",
          background: "var(--bg)",
        }}
      >
        {visible.map(({ test, cell }) => (
          <div
            key={test.test_id}
            onClick={() => onOpenTest(test.test_id)}
            style={{
              display: "grid",
              gridTemplateColumns: "70px 1fr 80px",
              gap: 12,
              alignItems: "center",
              padding: "10px 16px",
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
                fontFamily: "var(--font-mono)",
                fontSize: 12.5,
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {decodeTestId(test.test_id)}
            </span>
            <span
              style={{
                textAlign: "right",
                fontFamily: "var(--font-mono)",
                fontSize: 12,
                color: "var(--fg-muted)",
              }}
            >
              {fmt.duration(cell.duration_ms)}
            </span>
          </div>
        ))}
        {visible.length === 0 && (
          <div
            style={{
              padding: 32,
              textAlign: "center",
              color: "var(--fg-muted)",
              fontSize: 13,
            }}
          >
            No tests match this filter.
          </div>
        )}
      </div>
    </div>
  );
}

function RunStat({
  label,
  value,
  accent,
  muted,
  noBorder,
}: {
  label: string;
  value: number;
  accent?: "danger" | "success" | null;
  muted?: boolean;
  noBorder?: boolean;
}) {
  const color =
    accent === "danger"
      ? "var(--danger)"
      : accent === "success"
        ? "var(--success)"
        : muted
          ? "var(--fg-muted)"
          : "var(--fg)";
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
          fontFamily: "var(--font-mono)",
        }}
      >
        {value}
      </div>
    </div>
  );
}
