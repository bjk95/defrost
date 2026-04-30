export type Instrument = "gauge" | "sum" | "histogram";

export interface MetricBucket {
  le: number; // +Inf for the open-ended top bucket
  count: number;
}

export interface MetricPoint {
  run_id: string;
  ts: string;
  attrs: Record<string, string>;
  value?: number;
  count?: number;
  sum?: number;
  min?: number;
  max?: number;
  buckets?: MetricBucket[];
}

export interface MetricSeries {
  name: string;
  description?: string;
  unit?: string;
  instrument: Instrument;
  temporality?: "delta" | "cumulative";
  monotonic?: boolean;
  points: MetricPoint[];
}

// ---- Wire shape (matches the JSON the Go server emits) ----
// The +Inf bucket is encoded as { le: null }; we normalize to Infinity
// so the rest of the code can use isFinite(le) directly.

interface WireBucket {
  le: number | null;
  count: number;
}

interface WireMetricPoint extends Omit<MetricPoint, "buckets"> {
  buckets?: WireBucket[];
}

interface WireMetricSeries extends Omit<MetricSeries, "points"> {
  points: WireMetricPoint[];
}

export function normalizeWireMetrics(wire: WireMetricSeries[]): MetricSeries[] {
  return wire.map((s) => ({
    ...s,
    points: s.points.map((p) => ({
      ...p,
      buckets: p.buckets?.map((b) => ({
        le: b.le == null ? Infinity : b.le,
        count: b.count,
      })),
    })),
  }));
}

// ---- Helpers consumed by the page ----

export function quantileFromHist(buckets: MetricBucket[], q: number): number {
  const total = buckets.reduce((a, b) => a + b.count, 0);
  if (total === 0) return 0;
  const target = total * q;
  let cum = 0;
  let prevLE = 0;
  for (const b of buckets) {
    if (cum + b.count >= target) {
      if (!isFinite(b.le)) return prevLE;
      const frac = (target - cum) / (b.count || 1);
      return prevLE + (b.le - prevLE) * frac;
    }
    cum += b.count;
    if (isFinite(b.le)) prevLE = b.le;
  }
  return prevLE;
}

export function mergeHistograms(pts: MetricPoint[]): {
  count: number;
  sum: number;
  min: number;
  max: number;
  buckets: MetricBucket[];
} {
  const first = pts[0]?.buckets ?? [];
  const buckets: MetricBucket[] = first.map((b) => ({ le: b.le, count: 0 }));
  let count = 0;
  let sum = 0;
  let min = Infinity;
  let max = -Infinity;
  for (const p of pts) {
    count += p.count ?? 0;
    sum += p.sum ?? 0;
    if (p.min != null && p.min < min) min = p.min;
    if (p.max != null && p.max > max) max = p.max;
    if (p.buckets) {
      for (let i = 0; i < buckets.length; i++) {
        buckets[i].count += p.buckets[i]?.count ?? 0;
      }
    }
  }
  return {
    count,
    sum,
    min: min === Infinity ? 0 : min,
    max: max === -Infinity ? 0 : max,
    buckets,
  };
}

export function isHigherBetterMetric(name: string): boolean {
  if (name.startsWith("eval.") && !name.includes("error")) return true;
  if (name.includes("coverage")) return true;
  if (name.includes("harmlessness")) return true;
  return false;
}

export function formatMetricValue(v: number | undefined | null, unit?: string): string {
  if (v == null || !isFinite(v)) return "—";
  if (unit === "By" || unit === "bytes") return formatBytes(v);
  if (unit === "ms") {
    if (v < 1) return v.toFixed(2) + "ms";
    if (v < 1000) return v.toFixed(0) + "ms";
    return (v / 1000).toFixed(2) + "s";
  }
  if (unit === "s") {
    if (v < 60) return v.toFixed(2) + "s";
    return (v / 60).toFixed(1) + "m";
  }
  if (unit === "1" || unit === "{score}") {
    if (v >= 0 && v <= 1) return (v * 100).toFixed(1) + "%";
    return v.toFixed(3);
  }
  if (unit === "{request}") return Math.round(v).toLocaleString();
  if (Math.abs(v) >= 1e6) return (v / 1e6).toFixed(2) + "M";
  if (Math.abs(v) >= 1e3) return (v / 1e3).toFixed(2) + "k";
  if (Math.abs(v) >= 1) return v.toFixed(2);
  return v.toFixed(3);
}

export function formatMetricDelta(v: number | null, unit?: string): string {
  if (v == null) return "—";
  const sign = v > 0 ? "+" : v < 0 ? "−" : "";
  return sign + formatMetricValue(Math.abs(v), unit);
}

function formatBytes(b: number): string {
  const units = ["B", "KB", "MB", "GB"];
  let i = 0;
  let v = b;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2) + " " + units[i];
}
