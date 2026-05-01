import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  dropHistory,
  getDropPlan,
  type DropPlan,
  type DropSelector,
} from "@/api";
import { fmt, SUPPRESSIONS_QUERY_KEY } from "@/lib/utils";
import { Icon } from "@/components/Icons";

type Scope = "all" | "traces" | "metrics";

function selectorFor(scope: Scope, beforeUTC: string): DropSelector {
  const sel: DropSelector = {
    drop_traces: scope === "all" || scope === "traces",
    drop_metrics: scope === "all" || scope === "metrics",
  };
  if (beforeUTC) sel.before_utc = beforeUTC;
  return sel;
}

function scopeLabel(scope: Scope): string {
  if (scope === "traces") return "traces only";
  if (scope === "metrics") return "metrics only";
  return "traces + metrics";
}

export function ManagementPage() {
  const [scope, setScope] = useState<Scope>("all");
  const [beforeUTC, setBeforeUTC] = useState("");
  const [confirmText, setConfirmText] = useState("");
  const qc = useQueryClient();

  const sel = selectorFor(scope, beforeUTC);
  const planQuery = useQuery({
    queryKey: ["drop-plan", scope, beforeUTC],
    queryFn: () => getDropPlan(sel),
    staleTime: 5_000,
  });

  const dropMut = useMutation({
    mutationFn: () => dropHistory(sel),
    onSuccess: () => {
      // Invalidate everything that reads run history; the data branch was
      // just rewritten, so cached responses are stale.
      qc.invalidateQueries({ queryKey: ["tests"] });
      qc.invalidateQueries({ queryKey: ["metrics"] });
      qc.invalidateQueries({ queryKey: SUPPRESSIONS_QUERY_KEY });
      qc.invalidateQueries({ queryKey: ["drop-plan"] });
      setConfirmText("");
    },
  });

  const plan = planQuery.data;
  const canDrop =
    !!plan &&
    !plan.branch_missing &&
    !plan.nothing &&
    confirmText === "drop" &&
    !dropMut.isPending;

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
        management
      </div>
      <h1
        style={{
          fontSize: 22,
          fontWeight: 500,
          letterSpacing: "-0.02em",
          margin: "0 0 6px",
        }}
      >
        Drop history
      </h1>
      <p
        style={{
          margin: "0 0 24px",
          color: "var(--fg-muted)",
          fontSize: 13,
          maxWidth: 640,
          lineHeight: 1.55,
        }}
      >
        Permanently rewrite the data branch via an orphan commit force-pushed with{" "}
        <code style={inlineCodeStyle}>--force-with-lease</code>. Suppressions and{" "}
        <code style={inlineCodeStyle}>README.md</code> are preserved; everything else under the
        chosen scope is dropped. <strong style={{ color: "var(--fg)" }}>This is irreversible.</strong>
      </p>

      <Section title="Scope">
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <ScopeOption
            value="all"
            label="Drop everything"
            sub="traces + metrics"
            selected={scope === "all"}
            onSelect={setScope}
          />
          <ScopeOption
            value="traces"
            label="Drop traces only"
            sub="keep metrics"
            selected={scope === "traces"}
            onSelect={setScope}
          />
          <ScopeOption
            value="metrics"
            label="Drop metrics only"
            sub="keep traces"
            selected={scope === "metrics"}
            onSelect={setScope}
          />
        </div>
      </Section>

      <Section title="Date filter">
        <p style={{ margin: "0 0 12px", fontSize: 13, color: "var(--fg-muted)", maxWidth: 640 }}>
          Optional. Drop only runs whose UTC date is strictly{" "}
          <strong style={{ color: "var(--fg)" }}>before</strong> this date. Files dated on or
          after the cutoff are kept. Leave blank to drop everything in scope.
        </p>
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <input
            type="date"
            value={beforeUTC}
            onChange={(e) => setBeforeUTC(e.target.value)}
            disabled={dropMut.isPending}
            max={today()}
            style={{
              padding: "6px 10px",
              fontSize: 13,
              fontFamily: "var(--font-mono)",
              background: "var(--bg)",
              color: "var(--fg)",
              border: "1px solid var(--border)",
              borderRadius: 6,
              colorScheme: "dark light",
            }}
          />
          {beforeUTC && (
            <button
              onClick={() => setBeforeUTC("")}
              disabled={dropMut.isPending}
              style={{
                padding: "6px 10px",
                fontSize: 12,
                background: "transparent",
                color: "var(--fg-muted)",
                border: "1px solid var(--border)",
                borderRadius: 6,
                cursor: "pointer",
              }}
            >
              Clear
            </button>
          )}
        </div>
      </Section>

      <Section title="Plan">
        {planQuery.isLoading ? (
          <PlanState>
            <Spinner /> <span>Inventorying data branch…</span>
          </PlanState>
        ) : planQuery.isError ? (
          <PlanState tone="danger">
            Failed to load plan: {(planQuery.error as Error).message}
          </PlanState>
        ) : plan?.branch_missing ? (
          <PlanState>
            <Icon.Check /> Nothing to drop — branch{" "}
            <code style={inlineCodeStyle}>{plan.branch}</code> doesn't exist on origin yet.
          </PlanState>
        ) : plan?.nothing ? (
          <PlanState>
            <Icon.Check /> Nothing to drop in this scope.
          </PlanState>
        ) : plan ? (
          <PlanTable plan={plan} />
        ) : null}
      </Section>

      <Section title="Confirm">
        <p style={{ margin: "0 0 12px", fontSize: 13, color: "var(--fg-muted)" }}>
          Type <code style={inlineCodeStyle}>drop</code> to enable the button.
        </p>
        <div style={{ display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap" }}>
          <input
            type="text"
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder="drop"
            disabled={!plan || plan.branch_missing || plan.nothing || dropMut.isPending}
            style={{
              padding: "6px 10px",
              fontSize: 13,
              fontFamily: "var(--font-mono)",
              background: "var(--bg)",
              color: "var(--fg)",
              border: "1px solid var(--border)",
              borderRadius: 6,
              width: 120,
            }}
          />
          <button
            onClick={() => dropMut.mutate()}
            disabled={!canDrop}
            style={{
              padding: "6px 14px",
              fontSize: 13,
              fontWeight: 500,
              background: canDrop ? "var(--danger)" : "var(--bg-muted)",
              color: canDrop ? "white" : "var(--fg-subtle)",
              border: "1px solid",
              borderColor: canDrop ? "var(--danger)" : "var(--border)",
              borderRadius: 6,
              cursor: canDrop ? "pointer" : "not-allowed",
              display: "inline-flex",
              alignItems: "center",
              gap: 6,
            }}
          >
            {dropMut.isPending ? (
              <>
                <Spinner /> Rewriting branch…
              </>
            ) : (
              <>
                Drop {scopeLabel(scope)}
                {beforeUTC && <> before {fmt.absDateUTC(beforeUTC)}</>}
              </>
            )}
          </button>
          {dropMut.isError && (
            <span style={{ fontSize: 12, color: "var(--danger)", fontFamily: "var(--font-mono)" }}>
              {(dropMut.error as Error).message}
            </span>
          )}
          {dropMut.isSuccess && (
            <span style={{ fontSize: 12, color: "var(--success)", fontFamily: "var(--font-mono)" }}>
              Dropped — branch rewritten.
            </span>
          )}
        </div>
        <style>{`@keyframes defrostMgmtSpin { to { transform: rotate(360deg); } }`}</style>
      </Section>
    </div>
  );
}

function PlanTable({ plan }: { plan: DropPlan }) {
  return (
    <div
      style={{
        border: "1px solid var(--border)",
        borderRadius: 10,
        overflow: "hidden",
        background: "var(--bg)",
        maxWidth: 640,
      }}
    >
      <Row
        label="Branch"
        value={
          <span style={{ fontFamily: "var(--font-mono)" }}>
            {plan.branch}
            {plan.origin_url && (
              <span style={{ color: "var(--fg-muted)" }}> ({plan.origin_url})</span>
            )}
          </span>
        }
      />
      <Row
        label="Traces"
        value={
          <SignalLine
            files={plan.trace_files}
            bytes={plan.trace_bytes}
            preserved={!plan.drop_traces}
          />
        }
      />
      <Row
        label="Metrics"
        value={
          <SignalLine
            files={plan.metric_files}
            bytes={plan.metric_bytes}
            preserved={!plan.drop_metrics}
          />
        }
      />
      <Row
        label="Date range"
        value={
          plan.oldest_run_utc && plan.newest_run_utc
            ? `${fmt.absDateUTC(plan.oldest_run_utc)} → ${fmt.absDateUTC(plan.newest_run_utc)}`
            : "—"
        }
      />
      {plan.before_utc && (
        <Row
          label="Cutoff"
          value={
            <span>
              before{" "}
              <strong style={{ color: "var(--fg)" }}>{fmt.absDateUTC(plan.before_utc)}</strong>{" "}
              <span style={{ color: "var(--fg-muted)" }}>(UTC)</span>
            </span>
          }
        />
      )}
      <Row
        label="Preserved"
        value={
          <span style={{ color: "var(--fg-muted)" }}>
            <code style={inlineCodeStyle}>suppressions.json</code> ({plan.suppressions_n}{" "}
            {plan.suppressions_n === 1 ? "entry" : "entries"}),{" "}
            <code style={inlineCodeStyle}>README.md</code>
          </span>
        }
        last
      />
    </div>
  );
}

function SignalLine({
  files,
  bytes,
  preserved,
}: {
  files: number;
  bytes: number;
  preserved: boolean;
}) {
  if (preserved) {
    return (
      <span style={{ color: "var(--fg-muted)" }}>
        preserved · {files} files, {fmt.bytes(bytes)}
      </span>
    );
  }
  return (
    <span style={{ color: "var(--fg)" }}>
      <span style={{ color: "var(--danger)", fontWeight: 500 }}>drop</span> · {files} files,{" "}
      {fmt.bytes(bytes)}
    </span>
  );
}

function Row({
  label,
  value,
  last = false,
}: {
  label: string;
  value: React.ReactNode;
  last?: boolean;
}) {
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "120px 1fr",
        gap: 12,
        padding: "10px 16px",
        borderBottom: last ? "none" : "1px solid var(--border)",
        fontSize: 13,
        alignItems: "baseline",
      }}
    >
      <span style={{ color: "var(--fg-muted)", fontSize: 11, textTransform: "uppercase", letterSpacing: 0.06, fontWeight: 500 }}>
        {label}
      </span>
      <span style={{ color: "var(--fg)" }}>{value}</span>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section style={{ marginBottom: 28 }}>
      <h2
        style={{
          fontSize: 11,
          fontWeight: 500,
          letterSpacing: 0.06,
          textTransform: "uppercase",
          color: "var(--fg-muted)",
          margin: "0 0 12px",
        }}
      >
        {title}
      </h2>
      {children}
    </section>
  );
}

function ScopeOption({
  value,
  label,
  sub,
  selected,
  onSelect,
}: {
  value: Scope;
  label: string;
  sub: string;
  selected: boolean;
  onSelect: (s: Scope) => void;
}) {
  return (
    <label
      style={{
        display: "flex",
        gap: 12,
        alignItems: "center",
        padding: "10px 14px",
        border: "1px solid",
        borderColor: selected ? "var(--fg)" : "var(--border)",
        borderRadius: 8,
        cursor: "pointer",
        maxWidth: 480,
        background: selected ? "var(--bg-subtle)" : "var(--bg)",
        transition: "all var(--dur-fast) var(--ease-out)",
      }}
    >
      <input
        type="radio"
        name="drop-scope"
        checked={selected}
        onChange={() => onSelect(value)}
        style={{ accentColor: "var(--fg)" }}
      />
      <span style={{ display: "flex", flexDirection: "column", gap: 2 }}>
        <span style={{ fontSize: 13, color: "var(--fg)", fontWeight: 500 }}>{label}</span>
        <span style={{ fontSize: 11, color: "var(--fg-muted)", fontFamily: "var(--font-mono)" }}>
          {sub}
        </span>
      </span>
    </label>
  );
}

function PlanState({
  children,
  tone = "muted",
}: {
  children: React.ReactNode;
  tone?: "muted" | "danger";
}) {
  return (
    <div
      style={{
        padding: "16px 18px",
        border: "1px solid var(--border)",
        borderRadius: 8,
        fontSize: 13,
        color: tone === "danger" ? "var(--danger)" : "var(--fg-muted)",
        display: "flex",
        alignItems: "center",
        gap: 8,
        maxWidth: 640,
      }}
    >
      {children}
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
        animation: "defrostMgmtSpin 0.8s linear infinite",
      }}
    />
  );
}

const inlineCodeStyle: React.CSSProperties = {
  background: "var(--bg-muted)",
  padding: "1px 5px",
  borderRadius: 4,
  fontSize: 12,
  fontFamily: "var(--font-mono)",
};

function today(): string {
  const d = new Date();
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}
