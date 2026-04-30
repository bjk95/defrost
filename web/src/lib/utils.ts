import type { Cell, RunSummary, TestRow } from "@/types";

export const fmt = {
  relTime(iso: string): string {
    if (!iso) return "—";
    const t = new Date(iso).getTime();
    const now = Date.now();
    const diffSec = Math.round((now - t) / 1000);
    if (diffSec < 60) return diffSec + "s ago";
    const m = Math.round(diffSec / 60);
    if (m < 60) return m + "m ago";
    const h = Math.round(m / 60);
    if (h < 24) return h + "h ago";
    const d = Math.round(h / 24);
    if (d < 14) return d + "d ago";
    const w = Math.round(d / 7);
    return w + "w ago";
  },
  duration(ms: number): string {
    if (ms === 0) return "0";
    if (ms < 1) return "<1ms";
    if (ms < 1000) return ms + "ms";
    return (ms / 1000).toFixed(2) + "s";
  },
  durationShort(ms: number): string {
    if (ms < 1000) return ms + "ms";
    if (ms < 60_000) return (ms / 1000).toFixed(1) + "s";
    return Math.floor(ms / 60_000) + "m " + Math.floor((ms % 60_000) / 1000) + "s";
  },
  absTime(iso: string): string {
    if (!iso) return "—";
    const d = new Date(iso);
    return d.toLocaleString(undefined, {
      month: "short", day: "numeric", hour: "2-digit", minute: "2-digit",
    });
  },
  absDate(iso: string): string {
    if (!iso) return "—";
    const d = new Date(iso);
    return d.toLocaleDateString(undefined, {
      year: "numeric", month: "short", day: "numeric",
    });
  },
  initials(name?: string): string {
    if (!name) return "??";
    const parts = name.trim().split(/\s+/).filter(Boolean);
    if (parts.length === 0) return "??";
    if (parts.length === 1) return parts[0].slice(0, 2).toLowerCase();
    return (parts[0][0] + parts[parts.length - 1][0]).toLowerCase();
  },
};

export interface TestStats {
  pass: number;
  fail: number;
  skip: number;
  total: number;
  ran: number;
  lastStatus: string;
  failRate: number;
  p50: number;
  p95: number;
}

export function testStats(cells: Cell[]): TestStats {
  const ran = cells.filter((c) => c.status !== "skip");
  const pass = ran.filter((c) => c.status === "pass").length;
  const fail = ran.filter((c) => c.status === "fail").length;
  const skip = cells.length - ran.length;
  const lastNonSkip = [...cells].reverse().find((c) => c.status !== "skip");
  const failRate = ran.length ? fail / ran.length : 0;
  const durations = ran.map((c) => c.duration_ms).sort((a, b) => a - b);
  const p50 = durations.length ? durations[Math.floor(durations.length * 0.5)] : 0;
  const p95 = durations.length
    ? durations[Math.min(durations.length - 1, Math.floor(durations.length * 0.95))]
    : 0;
  return {
    pass, fail, skip,
    total: cells.length,
    ran: ran.length,
    lastStatus: lastNonSkip?.status ?? "skip",
    failRate, p50, p95,
  };
}

export type TreeNode = TreeBranch | TreeLeaf;

export interface TreeBranch {
  kind: "branch";
  name: string;
  path: string;
  depth: number;
  children: TreeNode[];
  tests: TestRow[];
}

export interface TreeLeaf {
  kind: "leaf";
  name: string;
  path: string;
  depth: number;
  test: TestRow;
}

const SEPARATOR_RE = /\/|\.|::| > /;

export function decodeTestId(id: string): string {
  try {
    return decodeURIComponent(id);
  } catch {
    return id;
  }
}

// Build an N-level tree: each split of test_id on `/`, `.`, `::`, or ` > `
// becomes a tree level. Branches with multiple children represent
// repos/packages; leaves are the individual tests.
export function buildTestTree(tests: TestRow[]): TreeBranch {
  const root: TreeBranch = {
    kind: "branch", name: "", path: "", depth: -1,
    children: [], tests: [],
  };
  for (const t of tests) {
    const decoded = decodeTestId(t.test_id);
    const segments = decoded.split(SEPARATOR_RE).filter((s) => s.length > 0);
    if (segments.length === 0) continue;

    let cur: TreeBranch = root;
    cur.tests.push(t);
    for (let i = 0; i < segments.length - 1; i++) {
      const seg = segments[i];
      const subPath = cur.path ? cur.path + "/" + seg : seg;
      let child = cur.children.find(
        (c) => c.kind === "branch" && c.name === seg,
      ) as TreeBranch | undefined;
      if (!child) {
        child = {
          kind: "branch", name: seg, path: subPath, depth: i,
          children: [], tests: [],
        };
        cur.children.push(child);
      }
      child.tests.push(t);
      cur = child;
    }
    const leafName = segments[segments.length - 1];
    const leafPath = cur.path ? cur.path + "/" + leafName : leafName;
    cur.children.push({
      kind: "leaf", name: leafName, path: leafPath,
      depth: segments.length - 1, test: t,
    });
  }
  sortNodeChildren(root);
  return root;
}

function sortNodeChildren(node: TreeBranch) {
  node.children.sort((a, b) => {
    if (a.kind !== b.kind) return a.kind === "branch" ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
  for (const c of node.children) {
    if (c.kind === "branch") sortNodeChildren(c);
  }
}

export interface RunCounts {
  pass: number; fail: number; skip: number; total: number; total_ms: number;
}

export function runCounts(tests: TestRow[], runId: string): RunCounts {
  let pass = 0, fail = 0, skip = 0, total = 0, total_ms = 0;
  for (const t of tests) {
    const c = t.cells.find((c) => c.run_id === runId);
    if (!c) continue;
    total++;
    total_ms += c.duration_ms;
    if (c.status === "pass") pass++;
    else if (c.status === "fail") fail++;
    else if (c.status === "skip") skip++;
  }
  return { pass, fail, skip, total, total_ms };
}

export interface SuppressionEntry {
  test_id: string;
  added_at: string;
  by?: string;
}

const SUPPRESS_KEY = "defrost.suppressions.v2";
const SUPPRESS_KEY_V1 = "defrost.suppressions.v1";
type Listener = () => void;

function loadInitial(): Map<string, SuppressionEntry> {
  const out = new Map<string, SuppressionEntry>();
  if (typeof localStorage === "undefined") return out;
  try {
    const raw = JSON.parse(localStorage.getItem(SUPPRESS_KEY) || "[]");
    if (Array.isArray(raw)) {
      for (const item of raw) {
        if (item && typeof item === "object" && typeof item.test_id === "string") {
          out.set(item.test_id, {
            test_id: item.test_id,
            added_at: typeof item.added_at === "string" ? item.added_at : "",
            by: typeof item.by === "string" ? item.by : undefined,
          });
        }
      }
    }
  } catch { /* ignored */ }
  if (out.size === 0) {
    try {
      const v1 = JSON.parse(localStorage.getItem(SUPPRESS_KEY_V1) || "[]");
      if (Array.isArray(v1)) {
        for (const id of v1) {
          if (typeof id === "string") {
            out.set(id, { test_id: id, added_at: "", by: undefined });
          }
        }
      }
    } catch { /* ignored */ }
  }
  return out;
}

function makeSuppressionStore() {
  const entries = loadInitial();
  const listeners = new Set<Listener>();
  function persist() {
    if (typeof localStorage === "undefined") return;
    try {
      localStorage.setItem(SUPPRESS_KEY, JSON.stringify([...entries.values()]));
    } catch { /* ignored */ }
  }
  function emit() { for (const l of listeners) l(); }
  return {
    has(id: string) { return entries.has(id); },
    list(): string[] { return [...entries.keys()]; },
    entries(): SuppressionEntry[] {
      // newest first
      return [...entries.values()].sort((a, b) => {
        if (a.added_at === b.added_at) return 0;
        if (!a.added_at) return 1;
        if (!b.added_at) return -1;
        return b.added_at.localeCompare(a.added_at);
      });
    },
    count(): number { return entries.size; },
    add(id: string, by?: string) {
      if (entries.has(id)) return;
      entries.set(id, { test_id: id, added_at: new Date().toISOString(), by });
      persist();
      emit();
    },
    remove(id: string) {
      if (!entries.has(id)) return;
      entries.delete(id);
      persist();
      emit();
    },
    subscribe(fn: Listener) { listeners.add(fn); return () => { listeners.delete(fn); }; },
  };
}

export const suppression = makeSuppressionStore();

export const cn = (...xs: Array<string | false | null | undefined>) =>
  xs.filter(Boolean).join(" ");

// Map a per-run cell lookup so consumers can do byRun.get(runId).
export function cellsByRun(cells: Cell[]): Map<string, Cell> {
  return new Map(cells.map((c) => [c.run_id, c] as const));
}

// Compute the worst status across cells in a run (fail > pass > skip).
export function worstStatus(cells: Cell[]): string | undefined {
  let s: string | undefined;
  for (const c of cells) {
    if (c.status === "fail") return "fail";
    if (c.status === "pass") s = s === "fail" ? s : "pass";
    else if (!s) s = "skip";
  }
  return s;
}

export type { Cell, RunSummary, TestRow };
