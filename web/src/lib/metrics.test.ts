import { describe, it, expect } from "vitest";
import {
  normalizeWireMetrics,
  quantileFromHist,
  mergeHistograms,
  isHigherBetterMetric,
  formatMetricValue,
  formatMetricDelta,
} from "./metrics";
import type { MetricBucket, MetricPoint, MetricSeries } from "./metrics";

describe("normalizeWireMetrics", () => {
  it("rewrites le=null to Infinity", () => {
    const wire: any = [
      {
        name: "x",
        instrument: "histogram",
        points: [
          { run_id: "r", ts: "t", attrs: {}, buckets: [{ le: 1, count: 1 }, { le: null, count: 2 }] },
        ],
      },
    ];
    const got = normalizeWireMetrics(wire);
    expect(got[0].points[0].buckets![0].le).toBe(1);
    expect(got[0].points[0].buckets![1].le).toBe(Infinity);
  });

  it("preserves shape when buckets undefined", () => {
    const wire: any = [{ name: "g", instrument: "gauge", points: [{ run_id: "r", ts: "t", attrs: {}, value: 5 }] }];
    const got = normalizeWireMetrics(wire);
    expect(got[0].points[0].value).toBe(5);
    expect(got[0].points[0].buckets).toBeUndefined();
  });
});

describe("quantileFromHist", () => {
  const buckets: MetricBucket[] = [
    { le: 1, count: 2 },
    { le: 2, count: 2 },
    { le: 5, count: 4 },
    { le: Infinity, count: 2 },
  ];

  it("returns 0 for empty histogram", () => {
    expect(quantileFromHist([], 0.5)).toBe(0);
  });

  it("interpolates within a bucket", () => {
    const v = quantileFromHist(buckets, 0.5);
    expect(v).toBeGreaterThan(0);
    expect(v).toBeLessThanOrEqual(5);
  });

  it("returns prevLE when target falls in +Inf bucket", () => {
    const v = quantileFromHist(buckets, 0.99);
    expect(v).toBe(5);
  });
});

describe("mergeHistograms", () => {
  it("sums counts and sums per-bucket", () => {
    const pts: MetricPoint[] = [
      {
        run_id: "1", ts: "t", attrs: {}, count: 3, sum: 6, min: 1, max: 4,
        buckets: [{ le: 2, count: 2 }, { le: Infinity, count: 1 }],
      },
      {
        run_id: "2", ts: "t", attrs: {}, count: 2, sum: 4, min: 2, max: 5,
        buckets: [{ le: 2, count: 1 }, { le: Infinity, count: 1 }],
      },
    ];
    const m = mergeHistograms(pts);
    expect(m.count).toBe(5);
    expect(m.sum).toBe(10);
    expect(m.min).toBe(1);
    expect(m.max).toBe(5);
    expect(m.buckets[0].count).toBe(3);
    expect(m.buckets[1].count).toBe(2);
  });

  it("returns zeros for empty input", () => {
    const m = mergeHistograms([]);
    expect(m.count).toBe(0);
    expect(m.min).toBe(0);
    expect(m.max).toBe(0);
    expect(m.buckets.length).toBe(0);
  });
});

describe("isHigherBetterMetric", () => {
  it("eval.* without error is higher-better", () => expect(isHigherBetterMetric("eval.score")).toBe(true));
  it("eval.error.* is not higher-better", () => expect(isHigherBetterMetric("eval.error.rate")).toBe(false));
  it("coverage is higher-better", () => expect(isHigherBetterMetric("test.coverage")).toBe(true));
  it("harmlessness is higher-better", () => expect(isHigherBetterMetric("model.harmlessness")).toBe(true));
  it("latency is not higher-better", () => expect(isHigherBetterMetric("request.latency_ms")).toBe(false));
});

describe("formatMetricValue", () => {
  it("nullish -> dash", () => expect(formatMetricValue(undefined)).toBe("—"));
  it("non-finite -> dash", () => expect(formatMetricValue(Infinity)).toBe("—"));
  it("ms under 1 → 2dp", () => expect(formatMetricValue(0.5, "ms")).toBe("0.50ms"));
  it("ms under 1000 → 0dp", () => expect(formatMetricValue(250, "ms")).toBe("250ms"));
  it("ms 1000+ → seconds", () => expect(formatMetricValue(2500, "ms")).toBe("2.50s"));
  it("seconds under 60 → s", () => expect(formatMetricValue(30, "s")).toBe("30.00s"));
  it("seconds 60+ → m", () => expect(formatMetricValue(120, "s")).toBe("2.0m"));
  it("score in 0..1 → percent", () => expect(formatMetricValue(0.873, "1")).toBe("87.3%"));
  it("score outside 0..1 → 3dp", () => expect(formatMetricValue(1.5, "1")).toBe("1.500"));
  it("bytes formatting", () => expect(formatMetricValue(2048, "By")).toMatch(/KB$/));
  it("requests integer", () => expect(formatMetricValue(1234, "{request}")).toMatch(/^1.?234$/));
  it("large default → M", () => expect(formatMetricValue(2_500_000)).toMatch(/M$/));
  it("medium default → k", () => expect(formatMetricValue(2500)).toMatch(/k$/));
});

describe("formatMetricDelta", () => {
  it("null -> dash", () => expect(formatMetricDelta(null)).toBe("—"));
  it("positive prefixed +", () => expect(formatMetricDelta(2.5)).toMatch(/^\+/));
  it("negative prefixed −", () => expect(formatMetricDelta(-2.5)).toMatch(/^−/));
  it("zero no prefix", () => expect(formatMetricDelta(0)).not.toMatch(/^[+−]/));
});

const _shapeCheck: MetricSeries = {
  name: "x",
  instrument: "gauge",
  points: [],
};
void _shapeCheck;
