import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getMetrics, getTests } from "@/api";
import { fmt } from "@/lib/utils";
import {
  formatMetricDelta,
  formatMetricValue,
  isHigherBetterMetric,
  mergeHistograms,
  quantileFromHist,
  type Instrument,
  type MetricBucket,
  type MetricPoint,
  type MetricSeries,
} from "@/lib/metrics";
import type { RunSummary } from "@/types";
import { SearchInput } from "@/components/Controls";
import { SectionLabel } from "@/components/Primitives";

export function MetricsPage() {
  const navigate = useNavigate();
  const tests = useQuery({ queryKey: ["tests"], queryFn: getTests });
  const metricsQuery = useQuery({ queryKey: ["metrics"], queryFn: getMetrics });

  const metrics = metricsQuery.data ?? [];
  const runsNewestFirst = tests.data?.runs ?? [];

  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [search, setSearch] = useState("");

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return metrics;
    return metrics.filter(
      (m) =>
        m.name.toLowerCase().includes(q) ||
        (m.description ?? "").toLowerCase().includes(q),
    );
  }, [search, metrics]);

  const groups = useMemo(() => groupMetrics(filtered), [filtered]);

  const selected =
    metrics.find((m) => m.name === selectedName) ?? metrics[0] ?? null;

  const isLoading = tests.isLoading || metricsQuery.isLoading;
  const error = (tests.error ?? metricsQuery.error) as Error | null;

  if (isLoading)
    return <p style={{ color: "var(--fg-muted)", fontSize: 13 }}>loading…</p>;
  if (error)
    return (
      <p style={{ color: "var(--danger)", fontSize: 13 }}>
        failed: {error.message}
      </p>
    );

  if (metrics.length === 0) {
    return (
      <div style={{ padding: "48px 24px", textAlign: "center", color: "var(--fg-muted)" }}>
        No metrics ingested yet — defrost will populate this view as soon as your tests emit OTel
        metrics.
      </div>
    );
  }

  const onOpenRun = (rid: string) =>
    navigate(`/run?id=${encodeURIComponent(rid)}`);

  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "300px minmax(0, 1fr)",
        gap: 24,
        alignItems: "start",
      }}
    >
      <aside
        style={{
          position: "sticky",
          top: 76,
          border: "1px solid var(--border)",
          borderRadius: 10,
          background: "var(--bg)",
          overflow: "hidden",
          maxHeight: "calc(100vh - 100px)",
          display: "flex",
          flexDirection: "column",
        }}
      >
        <div style={{ padding: 12, borderBottom: "1px solid var(--border)" }}>
          <SearchInput value={search} onChange={setSearch} placeholder="Filter metrics…" />
        </div>
        <div style={{ overflowY: "auto", flex: 1 }}>
          {groups.map((g) => (
            <MetricGroup
              key={g.name}
              group={g}
              selectedName={selected?.name ?? null}
              onSelect={setSelectedName}
            />
          ))}
          {filtered.length === 0 && (
            <div
              style={{
                padding: 24,
                color: "var(--fg-muted)",
                fontSize: 12,
                textAlign: "center",
              }}
            >
              No metrics match.
            </div>
          )}
        </div>
        <div
          style={{
            padding: "8px 12px",
            borderTop: "1px solid var(--border)",
            fontSize: 11,
            color: "var(--fg-muted)",
            fontFamily: "var(--font-mono)",
          }}
        >
          {filtered.length} of {metrics.length} metrics
        </div>
      </aside>

      <section style={{ minWidth: 0 }}>
        {selected && (
          <MetricDetail
            key={selected.name}
            metric={selected}
            runsNewestFirst={runsNewestFirst}
            onOpenRun={onOpenRun}
          />
        )}
      </section>
    </div>
  );
}

interface MetricGroupBucket {
  name: string;
  items: MetricSeries[];
}

function groupMetrics(metrics: MetricSeries[]): MetricGroupBucket[] {
  const map = new Map<string, MetricSeries[]>();
  for (const m of metrics) {
    const dot = m.name.indexOf(".");
    const key = dot === -1 ? m.name : m.name.slice(0, dot);
    if (!map.has(key)) map.set(key, []);
    map.get(key)!.push(m);
  }
  return [...map.entries()]
    .map(([name, items]) => ({
      name,
      items: items.slice().sort((a, b) => a.name.localeCompare(b.name)),
    }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

function MetricGroup({
  group,
  selectedName,
  onSelect,
}: {
  group: MetricGroupBucket;
  selectedName: string | null;
  onSelect: (name: string) => void;
}) {
  return (
    <div>
      <div
        style={{
          padding: "10px 14px 4px",
          fontSize: 10,
          fontWeight: 600,
          color: "var(--fg-muted)",
          textTransform: "uppercase",
          letterSpacing: 0.08,
          fontFamily: "var(--font-mono)",
        }}
      >
        {group.name}
      </div>
      {group.items.map((m) => (
        <MetricRow
          key={m.name}
          metric={m}
          active={m.name === selectedName}
          onSelect={() => onSelect(m.name)}
        />
      ))}
    </div>
  );
}

function MetricRow({
  metric,
  active,
  onSelect,
}: {
  metric: MetricSeries;
  active: boolean;
  onSelect: () => void;
}) {
  const dot = metric.name.indexOf(".");
  const shortName = dot === -1 ? metric.name : metric.name.slice(dot + 1);
  return (
    <button
      onClick={onSelect}
      style={{
        display: "grid",
        gridTemplateColumns: "1fr auto",
        alignItems: "center",
        gap: 8,
        width: "100%",
        padding: "8px 14px",
        textAlign: "left",
        background: active ? "var(--bg-muted)" : "transparent",
        border: "none",
        borderLeft: `2px solid ${active ? "var(--accent)" : "transparent"}`,
        cursor: "pointer",
        color: "var(--fg)",
        transition: "background var(--dur-fast) var(--ease-out)",
      }}
      onMouseEnter={(e) => {
        if (!active)
          (e.currentTarget as HTMLButtonElement).style.background = "var(--bg-subtle)";
      }}
      onMouseLeave={(e) => {
        if (!active)
          (e.currentTarget as HTMLButtonElement).style.background = "transparent";
      }}
    >
      <span
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          whiteSpace: "nowrap",
          overflow: "hidden",
          textOverflow: "ellipsis",
        }}
      >
        {shortName}
      </span>
      <InstrumentBadge instrument={metric.instrument} />
    </button>
  );
}

function instrumentColor(instrument: Instrument): string {
  if (instrument === "gauge") return "oklch(0.6 0.14 250)";
  if (instrument === "sum") return "oklch(0.62 0.13 150)";
  return "oklch(0.65 0.16 30)";
}

function instrumentLetter(instrument: Instrument): string {
  if (instrument === "gauge") return "G";
  if (instrument === "sum") return "Σ";
  return "H";
}

function instrumentLabel(instrument: Instrument): string {
  if (instrument === "gauge") return "Gauge";
  if (instrument === "sum") return "Sum";
  return "Histogram";
}

function InstrumentBadge({ instrument }: { instrument: Instrument }) {
  return (
    <span
      title={instrument}
      style={{
        width: 16,
        height: 16,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        borderRadius: 4,
        fontFamily: "var(--font-mono)",
        fontSize: 10,
        fontWeight: 600,
        background: instrumentColor(instrument),
        color: "white",
      }}
    >
      {instrumentLetter(instrument)}
    </span>
  );
}

function InstrumentPill({ instrument }: { instrument: Instrument }) {
  const c = instrumentColor(instrument);
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 5,
        padding: "2px 8px",
        borderRadius: 999,
        fontSize: 11,
        fontWeight: 500,
        background: `color-mix(in oklch, ${c} 16%, transparent)`,
        color: c,
        border: `1px solid color-mix(in oklch, ${c} 40%, transparent)`,
      }}
    >
      <span style={{ width: 5, height: 5, borderRadius: 999, background: c }} />
      {instrumentLabel(instrument)}
    </span>
  );
}

interface PerRunGauge {
  run: RunSummary;
  value: number;
}

interface PerRunHistogram {
  run: RunSummary;
  count: number;
  sum: number;
  min: number;
  max: number;
  buckets: MetricBucket[];
}

type PerRun = PerRunGauge | PerRunHistogram;

function isHistogramRow(p: PerRun): p is PerRunHistogram {
  return (p as PerRunHistogram).buckets !== undefined;
}

function MetricDetail({
  metric,
  runsNewestFirst,
  onOpenRun,
}: {
  metric: MetricSeries;
  runsNewestFirst: RunSummary[];
  onOpenRun: (rid: string) => void;
}) {
  const attrKeys = useMemo(() => collectAttrKeys(metric.points), [metric]);
  const [filters, setFilters] = useState<Record<string, Set<string>>>(() =>
    initFilters(attrKeys, metric.points),
  );
  useEffect(() => {
    setFilters(initFilters(attrKeys, metric.points));
    // Reset filters when the selected metric changes.
  }, [metric.name, attrKeys, metric.points]);

  const filteredPoints = useMemo(
    () => metric.points.filter((p) => matchesFilters(p, filters)),
    [metric.points, filters],
  );

  const perRun = useMemo<PerRun[]>(
    () => aggregatePerRun(filteredPoints, metric.instrument, runsNewestFirst),
    [filteredPoints, metric.instrument, runsNewestFirst],
  );

  return (
    <div>
      <MetricHeader metric={metric} />

      {attrKeys.length > 0 && (
        <FilterChips
          attrKeys={attrKeys}
          filters={filters}
          setFilters={setFilters}
          points={metric.points}
        />
      )}

      <StatsStrip metric={metric} perRun={perRun} />

      <div
        style={{
          marginTop: 18,
          border: "1px solid var(--border)",
          borderRadius: 10,
          padding: 16,
          background: "var(--bg)",
        }}
      >
        {metric.instrument === "histogram" ? (
          <HistogramHeatmap
            metric={metric}
            perRun={perRun.filter(isHistogramRow)}
            onOpenRun={onOpenRun}
          />
        ) : (
          <GaugeSumChart
            metric={metric}
            perRun={perRun.filter((p): p is PerRunGauge => !isHistogramRow(p))}
            onOpenRun={onOpenRun}
          />
        )}
      </div>

      <AttributeBreakdown
        metric={metric}
        filters={filters}
        runsNewestFirst={runsNewestFirst}
      />
    </div>
  );
}

function MetricHeader({ metric }: { metric: MetricSeries }) {
  return (
    <div style={{ marginBottom: 16 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 6, flexWrap: "wrap" }}>
        <h1
          style={{
            margin: 0,
            fontFamily: "var(--font-mono)",
            fontSize: 20,
            fontWeight: 600,
            letterSpacing: "-0.01em",
            wordBreak: "break-all",
          }}
        >
          {metric.name}
        </h1>
        <InstrumentPill instrument={metric.instrument} />
        {metric.unit && (
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              padding: "2px 7px",
              border: "1px solid var(--border)",
              borderRadius: 999,
              color: "var(--fg-muted)",
            }}
          >
            {metric.unit}
          </span>
        )}
        {metric.temporality && (
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>
            {metric.temporality}
            {metric.monotonic ? " · monotonic" : ""}
          </span>
        )}
      </div>
      {metric.description && (
        <p
          style={{
            margin: 0,
            color: "var(--fg-muted)",
            fontSize: 13,
            maxWidth: 720,
            lineHeight: 1.5,
          }}
        >
          {metric.description}
        </p>
      )}
    </div>
  );
}

// ---------- Filter chips ----------

function collectAttrKeys(points: MetricPoint[]): string[] {
  const keys = new Set<string>();
  for (const p of points) for (const k of Object.keys(p.attrs ?? {})) keys.add(k);
  return [...keys].sort();
}

function attrValueSet(points: MetricPoint[], key: string): string[] {
  const s = new Set<string>();
  for (const p of points) {
    const v = p.attrs?.[key];
    if (v != null) s.add(v);
  }
  return [...s].sort();
}

function initFilters(
  keys: string[],
  points: MetricPoint[],
): Record<string, Set<string>> {
  const f: Record<string, Set<string>> = {};
  for (const k of keys) f[k] = new Set(attrValueSet(points, k));
  return f;
}

function matchesFilters(
  point: MetricPoint,
  filters: Record<string, Set<string>>,
): boolean {
  for (const k of Object.keys(filters)) {
    const v = point.attrs?.[k];
    if (v == null) continue;
    if (!filters[k].has(v)) return false;
  }
  return true;
}

function FilterChips({
  attrKeys,
  filters,
  setFilters,
  points,
}: {
  attrKeys: string[];
  filters: Record<string, Set<string>>;
  setFilters: (f: Record<string, Set<string>>) => void;
  points: MetricPoint[];
}) {
  return (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 14, marginBottom: 16 }}>
      {attrKeys.map((k) => {
        const values = attrValueSet(points, k);
        if (values.length <= 1) return null;
        return (
          <div key={k} style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
            <span
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 11,
                color: "var(--fg-muted)",
                padding: "0 4px",
              }}
            >
              {k}
            </span>
            {values.map((v) => {
              const active = filters[k]?.has(v) ?? false;
              return (
                <button
                  key={v}
                  onClick={() => {
                    const next: Record<string, Set<string>> = {
                      ...filters,
                      [k]: new Set(filters[k]),
                    };
                    if (active) {
                      if (next[k].size > 1) next[k].delete(v);
                    } else {
                      next[k].add(v);
                    }
                    setFilters(next);
                  }}
                  style={{
                    padding: "3px 9px",
                    borderRadius: 999,
                    fontSize: 11,
                    fontFamily: "var(--font-mono)",
                    border: "1px solid " + (active ? "var(--accent)" : "var(--border)"),
                    background: active
                      ? "color-mix(in oklch, var(--accent) 14%, var(--bg))"
                      : "var(--bg)",
                    color: active ? "var(--fg)" : "var(--fg-muted)",
                    cursor: "pointer",
                  }}
                >
                  {v}
                </button>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}

// ---------- Aggregate per-run ----------

function aggregatePerRun(
  points: MetricPoint[],
  instrument: Instrument,
  runsNewestFirst: RunSummary[],
): PerRun[] {
  const byRun = new Map<string, MetricPoint[]>();
  for (const p of points) {
    if (!byRun.has(p.run_id)) byRun.set(p.run_id, []);
    byRun.get(p.run_id)!.push(p);
  }

  const out: PerRun[] = [];
  const isGauge = instrument === "gauge";
  // runsNewestFirst is ordered newest -> oldest; we want oldest -> newest left -> right.
  for (let i = runsNewestFirst.length - 1; i >= 0; i--) {
    const run = runsNewestFirst[i];
    const pts = byRun.get(run.run_id);
    if (!pts || pts.length === 0) continue;
    if (instrument === "histogram") {
      const merged = mergeHistograms(pts);
      out.push({ run, ...merged });
    } else {
      const total = pts.reduce((acc, p) => acc + (p.value ?? 0), 0);
      const value = isGauge ? total / pts.length : total;
      out.push({ run, value });
    }
  }
  return out;
}

// ---------- Stats strip ----------

interface Stat {
  label: string;
  value: string;
  deltaSign?: number;
  deltaInverted?: boolean;
}

function StatsStrip({ metric, perRun }: { metric: MetricSeries; perRun: PerRun[] }) {
  if (perRun.length === 0) return null;

  let stats: Stat[];
  if (metric.instrument === "histogram") {
    const last = perRun[perRun.length - 1];
    if (!isHistogramRow(last)) return null;
    const p50 = quantileFromHist(last.buckets, 0.5);
    const p95Raw = quantileFromHist(last.buckets, 0.95);
    const p95 = last.max != null ? Math.min(p95Raw, last.max) : p95Raw;
    const prev = perRun[perRun.length - 2];
    const prevHist = prev && isHistogramRow(prev) ? prev : null;
    const prevP95Raw = prevHist ? quantileFromHist(prevHist.buckets, 0.95) : null;
    const prevP95 =
      prevP95Raw != null && prevHist?.max != null
        ? Math.min(prevP95Raw, prevHist.max)
        : prevP95Raw;
    const delta = prevP95 != null ? p95 - prevP95 : null;
    stats = [
      { label: "count", value: last.count.toLocaleString() },
      { label: "p50", value: formatMetricValue(p50, metric.unit) },
      { label: "p95", value: formatMetricValue(p95, metric.unit) },
      {
        label: "Δ p95",
        value: delta != null ? formatMetricDelta(delta, metric.unit) : "—",
        deltaSign: delta == null ? 0 : Math.sign(delta),
        deltaInverted: true,
      },
      { label: "max", value: formatMetricValue(last.max, metric.unit) },
    ];
  } else {
    const last = perRun[perRun.length - 1] as PerRunGauge;
    const prev = perRun[perRun.length - 2] as PerRunGauge | undefined;
    const values = (perRun as PerRunGauge[]).map((p) => p.value).slice().sort((a, b) => a - b);
    const p50 = values[Math.floor(values.length * 0.5)] ?? 0;
    const p95 =
      values[Math.min(values.length - 1, Math.floor(values.length * 0.95))] ?? 0;
    const delta = prev ? last.value - prev.value : null;
    const isHigherBetter = isHigherBetterMetric(metric.name);
    stats = [
      { label: "latest", value: formatMetricValue(last.value, metric.unit) },
      { label: "p50", value: formatMetricValue(p50, metric.unit) },
      { label: "p95", value: formatMetricValue(p95, metric.unit) },
      {
        label: "Δ vs prev",
        value: delta != null ? formatMetricDelta(delta, metric.unit) : "—",
        deltaSign: delta == null ? 0 : Math.sign(delta),
        deltaInverted: !isHigherBetter,
      },
    ];
  }

  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: `repeat(${stats.length}, 1fr)`,
        gap: 1,
        border: "1px solid var(--border)",
        borderRadius: 10,
        overflow: "hidden",
        background: "var(--border)",
      }}
    >
      {stats.map((s) => (
        <div key={s.label} style={{ padding: "12px 16px", background: "var(--bg)" }}>
          <div
            style={{
              fontSize: 10,
              color: "var(--fg-muted)",
              textTransform: "uppercase",
              letterSpacing: 0.06,
              fontWeight: 500,
              marginBottom: 4,
            }}
          >
            {s.label}
          </div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 18,
              fontWeight: 500,
              color: deltaColor(s),
            }}
          >
            {s.value}
          </div>
        </div>
      ))}
    </div>
  );
}

function deltaColor(s: Stat): string {
  if (s.deltaSign == null || s.deltaSign === 0) return "var(--fg)";
  const positive = s.deltaSign > 0;
  // deltaInverted=true means "up is bad" (e.g. latency).
  const goodDirection = positive === !s.deltaInverted;
  return goodDirection ? "var(--success)" : "var(--danger)";
}

// ---------- Gauge / sum chart ----------

function GaugeSumChart({
  metric,
  perRun,
  onOpenRun,
}: {
  metric: MetricSeries;
  perRun: PerRunGauge[];
  onOpenRun: (rid: string) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [w, setW] = useState(880);
  const [hov, setHov] = useState<number | null>(null);

  useEffect(() => {
    const node = ref.current;
    if (!node) return;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        setW(Math.max(360, Math.floor(e.contentRect.width)));
      }
    });
    ro.observe(node);
    return () => ro.disconnect();
  }, []);

  if (perRun.length === 0) {
    return (
      <div style={{ padding: 24, textAlign: "center", color: "var(--fg-muted)", fontSize: 12 }}>
        No points match the active filters.
      </div>
    );
  }

  const H = 240;
  const padT = 18;
  const padB = 32;
  const padL = 56;
  const padR = 16;
  const innerW = w - padL - padR;
  const innerH = H - padT - padB;
  const values = perRun.map((p) => p.value);
  const minV = Math.min(0, ...values);
  const maxV = Math.max(...values, minV + 1);
  const yMin = minV;
  const yMax = maxV + (maxV - minV) * 0.08;
  const yScale = (v: number) =>
    padT + innerH - ((v - yMin) / (yMax - yMin || 1)) * innerH;

  const N = perRun.length;
  const slot = innerW / Math.max(1, N);

  const ticks = [
    yMin,
    yMin + (yMax - yMin) * 0.25,
    yMin + (yMax - yMin) * 0.5,
    yMin + (yMax - yMin) * 0.75,
    yMax,
  ];

  const path = perRun
    .map((p, i) => {
      const x = padL + slot * (i + 0.5);
      const y = yScale(p.value);
      return (i === 0 ? "M" : "L") + x.toFixed(1) + " " + y.toFixed(1);
    })
    .join(" ");

  const areaPath =
    path +
    " L" +
    (padL + slot * (N - 0.5)).toFixed(1) +
    " " +
    (padT + innerH).toFixed(1) +
    " L" +
    (padL + slot * 0.5).toFixed(1) +
    " " +
    (padT + innerH).toFixed(1) +
    " Z";

  const sorted = [...values].sort((a, b) => a - b);
  const median = sorted[Math.floor(sorted.length * 0.5)] ?? 0;

  return (
    <div ref={ref} style={{ position: "relative", width: "100%" }}>
      <svg width={w} height={H} style={{ display: "block" }}>
        {ticks.map((t, i) => (
          <g key={i}>
            <line
              x1={padL}
              x2={w - padR}
              y1={yScale(t)}
              y2={yScale(t)}
              stroke="var(--border)"
              strokeWidth="1"
            />
            <text
              x={padL - 8}
              y={yScale(t) + 3}
              textAnchor="end"
              fontSize="10"
              fontFamily="var(--font-mono)"
              fill="var(--fg-muted)"
            >
              {formatMetricValue(t, metric.unit)}
            </text>
          </g>
        ))}

        <line
          x1={padL}
          x2={w - padR}
          y1={yScale(median)}
          y2={yScale(median)}
          stroke="var(--accent)"
          strokeDasharray="3 3"
          strokeWidth="1"
          opacity="0.55"
        />
        <text
          x={w - padR - 4}
          y={yScale(median) - 4}
          textAnchor="end"
          fontSize="10"
          fontFamily="var(--font-mono)"
          fill="var(--accent)"
        >
          median
        </text>

        <path d={areaPath} fill="color-mix(in oklch, var(--accent) 14%, transparent)" />
        <path
          d={path}
          fill="none"
          stroke="var(--accent)"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />

        {perRun.map((p, i) => {
          const x = padL + slot * (i + 0.5);
          const y = yScale(p.value);
          const isHov = i === hov;
          return (
            <g key={p.run.run_id}>
              <rect
                x={padL + slot * i}
                y={padT}
                width={slot}
                height={innerH}
                fill="transparent"
                onMouseEnter={() => setHov(i)}
                onMouseLeave={() => setHov(null)}
                onClick={() => onOpenRun(p.run.run_id)}
                style={{ cursor: "pointer" }}
              />
              <circle
                cx={x}
                cy={y}
                r={isHov ? 4 : 2.4}
                fill="var(--accent)"
                stroke="var(--bg)"
                strokeWidth={isHov ? 2 : 1}
                style={{ pointerEvents: "none", transition: "r var(--dur-fast)" }}
              />
            </g>
          );
        })}

        <line
          x1={padL}
          x2={w - padR}
          y1={padT + innerH}
          y2={padT + innerH}
          stroke="var(--border-strong)"
          strokeWidth="1"
        />
        {perRun.length > 0 && (
          <>
            <text
              x={padL}
              y={H - 10}
              fontSize="10"
              fontFamily="var(--font-mono)"
              fill="var(--fg-muted)"
            >
              {fmt.relTime(perRun[0].run.ts)}
            </text>
            <text
              x={w - padR}
              y={H - 10}
              fontSize="10"
              fontFamily="var(--font-mono)"
              fill="var(--fg-muted)"
              textAnchor="end"
            >
              {fmt.relTime(perRun[perRun.length - 1].run.ts)}
            </text>
          </>
        )}
      </svg>

      {hov !== null && perRun[hov] && (
        <PointTooltip
          point={perRun[hov]}
          unit={metric.unit}
          x={padL + slot * (hov + 0.5)}
          containerW={w}
        />
      )}
    </div>
  );
}

function PointTooltip({
  point,
  unit,
  x,
  containerW,
}: {
  point: PerRunGauge;
  unit?: string;
  x: number;
  containerW: number;
}) {
  const left = Math.max(8, Math.min(containerW - 240, x - 110));
  return (
    <div
      style={{
        position: "absolute",
        left,
        top: 8,
        pointerEvents: "none",
        background: "var(--bg)",
        border: "1px solid var(--border)",
        borderRadius: 8,
        padding: "8px 12px",
        boxShadow: "var(--shadow-md)",
        minWidth: 220,
        fontSize: 12,
      }}
    >
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 4 }}>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 14, fontWeight: 500 }}>
          {formatMetricValue(point.value, unit)}
        </span>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>
          {point.run.commit?.slice(0, 7)}
        </span>
      </div>
      <div style={{ color: "var(--fg-muted)", fontSize: 11, fontFamily: "var(--font-mono)" }}>
        {point.run.branch}
      </div>
      <div style={{ color: "var(--fg-muted)", fontSize: 11, marginTop: 2 }}>
        {fmt.relTime(point.run.ts)} · click to open run
      </div>
    </div>
  );
}

// ---------- Histogram heatmap ----------

function HistogramHeatmap({
  metric,
  perRun,
  onOpenRun,
}: {
  metric: MetricSeries;
  perRun: PerRunHistogram[];
  onOpenRun: (rid: string) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [w, setW] = useState(880);
  const [hov, setHov] = useState<{ col: number; row: number } | null>(null);

  useEffect(() => {
    const node = ref.current;
    if (!node) return;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        setW(Math.max(420, Math.floor(e.contentRect.width)));
      }
    });
    ro.observe(node);
    return () => ro.disconnect();
  }, []);

  if (perRun.length === 0) {
    return (
      <div style={{ padding: 24, textAlign: "center", color: "var(--fg-muted)", fontSize: 12 }}>
        No points match the active filters.
      </div>
    );
  }

  const buckets = perRun[0].buckets;
  const rows = [...buckets].reverse();
  const padT = 16;
  const padB = 36;
  const padL = 80;
  const padR = 16;
  const N = perRun.length;
  const cellW = Math.max(8, Math.floor((w - padL - padR) / N));
  const innerW = cellW * N;
  const cellH = 22;
  const H = padT + cellH * rows.length + padB;

  const cellFrac = (col: number, rowIdxFromBottom: number): number => {
    const p = perRun[col];
    const b = p.buckets[rowIdxFromBottom];
    return p.count > 0 ? (b?.count ?? 0) / p.count : 0;
  };

  const colP = perRun.map((p) => ({
    p50: quantileFromHist(p.buckets, 0.5),
    p95: quantileFromHist(p.buckets, 0.95),
  }));

  function valueToY(v: number | null | undefined): number {
    if (v == null) return padT;
    let prevLE = 0;
    for (let i = 0; i < buckets.length; i++) {
      const b = buckets[i];
      const upper = isFinite(b.le) ? b.le : prevLE * 4 + 1;
      if (v <= upper) {
        const frac = (v - prevLE) / Math.max(1e-9, upper - prevLE);
        const rowFromTop = rows.length - 1 - i;
        return padT + rowFromTop * cellH + (1 - frac) * cellH;
      }
      prevLE = isFinite(b.le) ? b.le : prevLE;
    }
    return padT;
  }

  return (
    <div ref={ref} style={{ position: "relative", width: "100%", overflowX: "auto" }}>
      <svg
        width={Math.max(w, padL + innerW + padR)}
        height={H}
        style={{ display: "block" }}
      >
        {rows.map((b, ri) => {
          const y = padT + ri * cellH;
          const labelLE = isFinite(b.le) ? "≤ " + formatMetricValue(b.le, metric.unit) : "+∞";
          return (
            <g key={ri}>
              <text
                x={padL - 8}
                y={y + cellH / 2 + 3}
                textAnchor="end"
                fontSize="10"
                fontFamily="var(--font-mono)"
                fill="var(--fg-muted)"
              >
                {labelLE}
              </text>
            </g>
          );
        })}

        {perRun.map((p, ci) =>
          rows.map((_, ri) => {
            const rowFromBottom = rows.length - 1 - ri;
            const frac = cellFrac(ci, rowFromBottom);
            const t = Math.sqrt(frac);
            const fill =
              t === 0
                ? "var(--bg-subtle)"
                : `color-mix(in oklch, var(--accent) ${(t * 100).toFixed(1)}%, var(--bg))`;
            const isHov = hov?.col === ci && hov?.row === ri;
            const bucket = p.buckets[rowFromBottom];
            return (
              <rect
                key={ci + "-" + ri}
                x={padL + ci * cellW}
                y={padT + ri * cellH}
                width={cellW - 1}
                height={cellH - 1}
                fill={fill}
                stroke={isHov ? "var(--accent)" : "transparent"}
                strokeWidth="1.5"
                onMouseEnter={() => setHov({ col: ci, row: ri })}
                onMouseLeave={() => setHov(null)}
                onClick={() => onOpenRun(p.run.run_id)}
                style={{ cursor: "pointer" }}
              >
                <title>
                  {`run ${p.run.commit?.slice(0, 7)} · ${rowLabel(
                    rows[ri],
                    metric.unit,
                  )}: ${bucket?.count ?? 0} (${(frac * 100).toFixed(1)}%)`}
                </title>
              </rect>
            );
          }),
        )}

        <polyline
          points={colP
            .map((c, i) => padL + i * cellW + cellW / 2 + "," + valueToY(c.p50))
            .join(" ")}
          fill="none"
          stroke="oklch(0.65 0.16 220)"
          strokeWidth="1.6"
          strokeDasharray="2 3"
          opacity="0.85"
        />
        <polyline
          points={colP
            .map((c, i) => padL + i * cellW + cellW / 2 + "," + valueToY(c.p95))
            .join(" ")}
          fill="none"
          stroke="oklch(0.62 0.20 28)"
          strokeWidth="1.6"
          opacity="0.85"
        />

        {perRun.length > 0 && (
          <>
            <text
              x={padL}
              y={H - 14}
              fontSize="10"
              fontFamily="var(--font-mono)"
              fill="var(--fg-muted)"
            >
              {fmt.relTime(perRun[0].run.ts)}
            </text>
            <text
              x={padL + innerW}
              y={H - 14}
              fontSize="10"
              fontFamily="var(--font-mono)"
              fill="var(--fg-muted)"
              textAnchor="end"
            >
              {fmt.relTime(perRun[perRun.length - 1].run.ts)}
            </text>
          </>
        )}

        <g transform={`translate(${padL + innerW - 200}, ${H - 28})`}>
          {[0.05, 0.2, 0.4, 0.6, 0.8, 1].map((t, i) => (
            <rect
              key={i}
              x={i * 14}
              y={0}
              width={13}
              height={8}
              fill={
                t === 0
                  ? "var(--bg-subtle)"
                  : `color-mix(in oklch, var(--accent) ${(Math.sqrt(t) * 100).toFixed(0)}%, var(--bg))`
              }
            />
          ))}
          <text
            x={0}
            y={20}
            fontSize="9"
            fontFamily="var(--font-mono)"
            fill="var(--fg-muted)"
          >
            0%
          </text>
          <text
            x={6 * 14 - 4}
            y={20}
            fontSize="9"
            fontFamily="var(--font-mono)"
            fill="var(--fg-muted)"
            textAnchor="end"
          >
            100%
          </text>
        </g>
        <g transform={`translate(${padL}, ${H - 22})`}>
          <line
            x1="0"
            y1="0"
            x2="14"
            y2="0"
            stroke="oklch(0.65 0.16 220)"
            strokeDasharray="2 3"
            strokeWidth="1.6"
          />
          <text x="18" y="3" fontSize="10" fontFamily="var(--font-mono)" fill="var(--fg-muted)">
            p50
          </text>
          <line x1="48" y1="0" x2="62" y2="0" stroke="oklch(0.62 0.20 28)" strokeWidth="1.6" />
          <text x="66" y="3" fontSize="10" fontFamily="var(--font-mono)" fill="var(--fg-muted)">
            p95
          </text>
        </g>
      </svg>

      {hov && perRun[hov.col] && (
        <HeatmapTooltip
          run={perRun[hov.col].run}
          row={rows[hov.row]}
          unit={metric.unit}
          count={perRun[hov.col].buckets[rows.length - 1 - hov.row]?.count ?? 0}
          frac={cellFrac(hov.col, rows.length - 1 - hov.row)}
          left={Math.max(8, Math.min(w - 240, padL + hov.col * cellW + cellW + 8))}
          top={padT + hov.row * cellH}
        />
      )}
    </div>
  );
}

function HeatmapTooltip({
  run,
  row,
  unit,
  count,
  frac,
  left,
  top,
}: {
  run: RunSummary;
  row: MetricBucket;
  unit?: string;
  count: number;
  frac: number;
  left: number;
  top: number;
}) {
  return (
    <div
      style={{
        position: "absolute",
        left,
        top,
        pointerEvents: "none",
        background: "var(--bg)",
        border: "1px solid var(--border)",
        borderRadius: 8,
        padding: "8px 12px",
        boxShadow: "var(--shadow-md)",
        minWidth: 200,
        fontSize: 12,
      }}
    >
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 4 }}>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>{rowLabel(row, unit)}</span>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>
          {run.commit?.slice(0, 7)}
        </span>
      </div>
      <div style={{ fontFamily: "var(--font-mono)", fontSize: 14, fontWeight: 500 }}>
        {count.toLocaleString()}{" "}
        <span style={{ color: "var(--fg-muted)", fontWeight: 400, fontSize: 12 }}>
          ({(frac * 100).toFixed(1)}%)
        </span>
      </div>
      <div style={{ color: "var(--fg-muted)", fontSize: 11, marginTop: 2 }}>
        {fmt.relTime(run.ts)} · click to open run
      </div>
    </div>
  );
}

function rowLabel(b: MetricBucket, unit?: string): string {
  return isFinite(b.le) ? "≤ " + formatMetricValue(b.le, unit) : "+∞";
}

// ---------- Attribute breakdown ----------

function AttributeBreakdown({
  metric,
  filters,
  runsNewestFirst,
}: {
  metric: MetricSeries;
  filters: Record<string, Set<string>>;
  runsNewestFirst: RunSummary[];
}) {
  const attrKeys = collectAttrKeys(metric.points);
  if (attrKeys.length === 0) return null;
  const lastRunId = runsNewestFirst[0]?.run_id;
  if (!lastRunId) return null;

  return (
    <div style={{ marginTop: 24 }}>
      <SectionLabel>Latest run · per attribute</SectionLabel>
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(280px, 1fr))",
          gap: 12,
        }}
      >
        {attrKeys.map((k) => {
          const values = attrValueSet(metric.points, k);
          if (values.length <= 1) return null;
          return (
            <div
              key={k}
              style={{
                border: "1px solid var(--border)",
                borderRadius: 10,
                background: "var(--bg)",
                overflow: "hidden",
              }}
            >
              <div
                style={{
                  padding: "8px 12px",
                  borderBottom: "1px solid var(--border)",
                  fontSize: 11,
                  color: "var(--fg-muted)",
                  fontFamily: "var(--font-mono)",
                }}
              >
                {k}
              </div>
              {values.map((v) => {
                const pts = metric.points.filter(
                  (p) => p.attrs?.[k] === v && p.run_id === lastRunId,
                );
                let display = "—";
                if (pts.length > 0) {
                  if (metric.instrument === "histogram") {
                    const merged = mergeHistograms(pts);
                    display =
                      formatMetricValue(quantileFromHist(merged.buckets, 0.5), metric.unit) +
                      " p50";
                  } else {
                    const sum = pts.reduce((a, p) => a + (p.value ?? 0), 0);
                    display = formatMetricValue(sum, metric.unit);
                  }
                }
                const dim = !filters[k]?.has(v);
                return (
                  <div
                    key={v}
                    style={{
                      display: "grid",
                      gridTemplateColumns: "1fr auto",
                      gap: 8,
                      padding: "8px 12px",
                      borderTop: "1px solid var(--border)",
                      fontSize: 13,
                      opacity: dim ? 0.4 : 1,
                    }}
                  >
                    <span
                      style={{
                        fontFamily: "var(--font-mono)",
                        fontSize: 12,
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                      }}
                    >
                      {v}
                    </span>
                    <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg-muted)" }}>
                      {display}
                    </span>
                  </div>
                );
              })}
            </div>
          );
        })}
      </div>
    </div>
  );
}
