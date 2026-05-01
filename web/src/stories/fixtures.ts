import type { GridResponse, RunRecord, RunSummary, TestRow, TestRunDetail } from "@/types";
import type { MetricSeries } from "@/lib/metrics";

const AUTHORS = [
  { name: "Beth Kemp", email: "beth@example.com" },
  { name: "Jules Park", email: "jules@example.com" },
  { name: "Sam Wu", email: "sam@example.com" },
  { name: "octocat", email: "octo@github.com" },
];

const BRANCHES = ["main", "main", "main", "main", "feat/score-eval", "fix/timeout"];

function pad(n: number) {
  return n < 10 ? "0" + n : "" + n;
}

function isoMinusHours(h: number, base = new Date("2026-04-30T18:00:00Z").getTime()) {
  return new Date(base - h * 3_600_000).toISOString();
}

function commitHash(seed: number): string {
  // Deterministic 40-hex-char string from a seed.
  const hex = "0123456789abcdef";
  let s = "";
  let n = seed;
  for (let i = 0; i < 40; i++) {
    n = (n * 1664525 + 1013904223) >>> 0;
    s += hex[n % 16];
  }
  return s;
}

export function makeRuns(count = 20): RunSummary[] {
  const out: RunSummary[] = [];
  for (let i = 0; i < count; i++) {
    const author = AUTHORS[i % AUTHORS.length];
    const branch = BRANCHES[i % BRANCHES.length];
    const commit = commitHash(i * 7 + 1);
    out.push({
      run_id: "run-" + pad(count - i),
      ts: isoMinusHours(i * 6),
      commit,
      parent: i + 1 < count ? commitHash((i + 1) * 7 + 1) : undefined,
      branch,
      pr: i % 4 === 1 ? 1200 + i : undefined,
      author_name: author.name,
      author_email: author.email,
      cmd: ["go", "test", "./..."],
      os: "darwin",
      arch: "arm64",
    });
  }
  return out; // newest first, matching server contract
}

interface TestSpec {
  test_id: string;
  test_name: string;
  // pattern: array of statuses, oldest -> newest, length should equal runs count
  pattern: ("pass" | "fail" | "skip" | undefined)[];
  baseDuration: number;
}

const SPECS: TestSpec[] = [
  {
    test_id: "github.com/acme/api/handlers.TestLogin",
    test_name: "TestLogin",
    pattern: Array(20).fill("pass"),
    baseDuration: 28,
  },
  {
    test_id: "github.com/acme/api/handlers.TestLogout",
    test_name: "TestLogout",
    pattern: Array(20).fill("pass"),
    baseDuration: 14,
  },
  {
    test_id: "github.com/acme/api/handlers.TestRefreshToken",
    test_name: "TestRefreshToken",
    pattern: ["pass","pass","pass","fail","pass","pass","pass","pass","fail","pass","pass","pass","pass","pass","pass","fail","pass","pass","pass","pass"],
    baseDuration: 42,
  },
  {
    test_id: "github.com/acme/api/handlers.TestRateLimit",
    test_name: "TestRateLimit",
    pattern: Array(20).fill("pass").map((_, i) => (i < 4 ? "fail" : "pass")) as TestSpec["pattern"],
    baseDuration: 320,
  },
  {
    test_id: "github.com/acme/api/store.TestCreateUser",
    test_name: "TestCreateUser",
    pattern: Array(20).fill("pass"),
    baseDuration: 96,
  },
  {
    test_id: "github.com/acme/api/store.TestQueryUsers",
    test_name: "TestQueryUsers",
    pattern: Array(20).fill("pass"),
    baseDuration: 18,
  },
  {
    test_id: "github.com/acme/api/store.TestMigrate",
    test_name: "TestMigrate",
    pattern: ["skip","skip","skip","skip","pass","pass","pass","pass","pass","pass","pass","pass","pass","pass","pass","pass","pass","pass","pass","pass"],
    baseDuration: 1240,
  },
  {
    test_id: "github.com/acme/api/store.TestDelete",
    test_name: "TestDelete",
    pattern: Array(20).fill("pass"),
    baseDuration: 22,
  },
  {
    test_id: "github.com/acme/eval.TestFactuality",
    test_name: "TestFactuality",
    pattern: Array(20).fill("pass"),
    baseDuration: 1850,
  },
  {
    test_id: "github.com/acme/eval.TestHarmlessness",
    test_name: "TestHarmlessness",
    pattern: Array(20).fill("pass"),
    baseDuration: 2210,
  },
  {
    test_id: "github.com/acme/eval.TestLatency",
    test_name: "TestLatency",
    pattern: Array(20).fill("pass"),
    baseDuration: 760,
  },
  {
    test_id: "github.com/acme/cli.TestRootCommand",
    test_name: "TestRootCommand",
    pattern: Array(20).fill("pass"),
    baseDuration: 6,
  },
  {
    test_id: "github.com/acme/cli.TestExec",
    test_name: "TestExec",
    pattern: Array(20).fill("pass"),
    baseDuration: 84,
  },
];

function patternStatus(spec: TestSpec, i: number): TestSpec["pattern"][number] {
  return spec.pattern[i];
}

export function makeTests(runs: RunSummary[]): TestRow[] {
  // runs are newest-first; the spec patterns are oldest-first, so reverse-index.
  const N = runs.length;
  return SPECS.map((spec) => {
    const cells = [];
    for (let i = 0; i < N; i++) {
      const run = runs[i];
      const oldestIdx = N - 1 - i;
      const status = patternStatus(spec, oldestIdx);
      if (!status) continue;
      const jitter = ((oldestIdx * 17) % 7) - 3;
      const dur =
        status === "skip"
          ? 0
          : Math.max(1, spec.baseDuration + jitter * (spec.baseDuration / 20));
      cells.push({
        run_id: run.run_id,
        status,
        duration_ms: Math.round(dur),
      });
    }
    return {
      test_id: spec.test_id,
      test_name: spec.test_name,
      cells,
    };
  });
}

export function makeGrid(): GridResponse {
  const runs = makeRuns(20);
  return { runs, tests: makeTests(runs) };
}

export function makeEmptyGrid(): GridResponse {
  return { runs: [], tests: [] };
}

// All-passing grid — useful for stories that should look "green and quiet".
export function makeGreenGrid(): GridResponse {
  const runs = makeRuns(20);
  const tests = SPECS.slice(0, 6).map((spec) => ({
    test_id: spec.test_id,
    test_name: spec.test_name,
    cells: runs.map((r, i) => ({
      run_id: r.run_id,
      status: "pass" as const,
      duration_ms: spec.baseDuration + ((i * 5) % 7),
    })),
  }));
  return { runs, tests };
}

export function makeTestRunDetail(testId: string, runId: string): TestRunDetail {
  const grid = makeGrid();
  const run = grid.runs.find((r) => r.run_id === runId) ?? grid.runs[0];
  const test = grid.tests.find((t) => t.test_id === testId) ?? grid.tests[0];
  const cell = test.cells.find((c) => c.run_id === run.run_id) ?? test.cells[0];

  const record: RunRecord = {
    schema: 1,
    run_id: run.run_id,
    commit: run.commit,
    parent: run.parent,
    branch: run.branch,
    pr: run.pr,
    author_email: run.author_email,
    author_name: run.author_name,
    dirty: false,
    cmd: run.cmd,
    cmd_hash: "abc123",
    go_version: "go1.23.4",
    os: run.os,
    arch: run.arch,
    ts: run.ts,
  };

  const output =
    cell.status === "fail"
      ? `=== RUN   ${test.test_name}
    handlers_test.go:142: expected 200, got 401
    --- FAIL: ${test.test_name} (0.04s)
FAIL    ${test.test_id} ${(cell.duration_ms / 1000).toFixed(3)}s`
      : `=== RUN   ${test.test_name}
--- PASS: ${test.test_name} (${(cell.duration_ms / 1000).toFixed(3)}s)
PASS    ${test.test_id} ${(cell.duration_ms / 1000).toFixed(3)}s`;

  return {
    test: {
      schema: 1,
      test_id: test.test_id,
      test_name: test.test_name,
      run_id: run.run_id,
      ts: run.ts,
      ran: cell.status !== "skip",
      passed: cell.status === "pass",
      status: cell.status,
      duration_ms: cell.duration_ms,
      output,
    },
    run: record,
  };
}

// ---- Metrics fixtures ----

function gaugeSeries(name: string, runs: RunSummary[], opts: {
  description?: string;
  unit?: string;
  baseline: number;
  amplitude: number;
  attrs?: Array<Record<string, string>>;
  drift?: number;
}): MetricSeries {
  const points = [];
  const attrSets = opts.attrs ?? [{}];
  for (let i = 0; i < runs.length; i++) {
    const oldestIdx = runs.length - 1 - i;
    for (const attrs of attrSets) {
      const phase = (oldestIdx + Object.values(attrs).join("").length) * 0.6;
      const value =
        opts.baseline +
        Math.sin(phase) * opts.amplitude +
        (opts.drift ?? 0) * oldestIdx;
      points.push({
        run_id: runs[i].run_id,
        ts: runs[i].ts,
        attrs,
        value,
      });
    }
  }
  return {
    name,
    description: opts.description,
    unit: opts.unit,
    instrument: "gauge",
    points,
  };
}

function sumSeries(name: string, runs: RunSummary[], opts: {
  description?: string;
  unit?: string;
  perRun: number;
}): MetricSeries {
  const points = runs.map((r, i) => ({
    run_id: r.run_id,
    ts: r.ts,
    attrs: {},
    value: opts.perRun + ((i * 9) % 11) * 12,
  }));
  return {
    name,
    description: opts.description,
    unit: opts.unit,
    instrument: "sum",
    temporality: "cumulative",
    monotonic: true,
    points,
  };
}

function histSeries(name: string, runs: RunSummary[], opts: {
  description?: string;
  unit?: string;
  ledges: number[];
}): MetricSeries {
  const points = runs.map((r, i) => {
    const oldestIdx = runs.length - 1 - i;
    // Concentrate weight in different buckets per run for visual variation.
    const peak = oldestIdx % opts.ledges.length;
    const buckets = opts.ledges.map((le, idx) => {
      const dist = Math.abs(idx - peak);
      const count = Math.max(0, 80 - dist * 22 + ((oldestIdx * 13) % 17));
      return { le, count };
    });
    buckets.push({ le: Infinity, count: 4 + (oldestIdx % 5) });
    const total = buckets.reduce((a, b) => a + b.count, 0);
    return {
      run_id: r.run_id,
      ts: r.ts,
      attrs: {},
      count: total,
      sum: total * (opts.ledges[peak] * 0.7),
      min: 1,
      max: opts.ledges[opts.ledges.length - 1] * 1.4,
      buckets,
    };
  });
  return {
    name,
    description: opts.description,
    unit: opts.unit,
    instrument: "histogram",
    points,
  };
}

export function makeMetrics(): MetricSeries[] {
  const runs = makeRuns(20);
  return [
    gaugeSeries("eval.factuality", runs, {
      description: "Mean factuality score across the harness suite.",
      unit: "{score}",
      baseline: 0.82,
      amplitude: 0.04,
      drift: -0.001,
      attrs: [{ model: "claude-opus-4-7" }, { model: "claude-sonnet-4-6" }],
    }),
    gaugeSeries("eval.harmlessness", runs, {
      description: "Mean harmlessness score across the harness suite.",
      unit: "{score}",
      baseline: 0.91,
      amplitude: 0.02,
    }),
    gaugeSeries("eval.latency_p95", runs, {
      description: "p95 latency, end to end, for the eval harness.",
      unit: "ms",
      baseline: 1450,
      amplitude: 220,
    }),
    sumSeries("eval.requests", runs, {
      description: "Total requests issued by the eval harness.",
      unit: "{request}",
      perRun: 4200,
    }),
    histSeries("http.server.duration", runs, {
      description: "Server-side request handling duration.",
      unit: "ms",
      ledges: [5, 10, 25, 50, 100, 250, 500, 1000],
    }),
  ];
}

export function makeEmptyMetrics(): MetricSeries[] {
  return [];
}
