import { describe, it, expect } from "vitest";
import {
  fmt,
  testStats,
  buildTestTree,
  decodeTestId,
  runCounts,
  cellsByRun,
  worstStatus,
  cn,
} from "./utils";
import type { Cell, TestRow } from "@/types";

describe("fmt.duration", () => {
  it("renders zero", () => expect(fmt.duration(0)).toBe("0"));
  it("renders sub-millisecond", () => expect(fmt.duration(0.5)).toBe("<1ms"));
  it("renders milliseconds", () => expect(fmt.duration(250)).toBe("250ms"));
  it("renders seconds with two decimals", () => expect(fmt.duration(2500)).toBe("2.50s"));
});

describe("fmt.durationShort", () => {
  it("ms under 1s", () => expect(fmt.durationShort(500)).toBe("500ms"));
  it("seconds with one decimal", () => expect(fmt.durationShort(2500)).toBe("2.5s"));
  it("minutes + seconds", () => expect(fmt.durationShort(125_000)).toBe("2m 5s"));
});

describe("fmt.initials", () => {
  it("undefined -> ??", () => expect(fmt.initials()).toBe("??"));
  it("empty -> ??", () => expect(fmt.initials("")).toBe("??"));
  it("single word", () => expect(fmt.initials("alice")).toBe("al"));
  it("two words", () => expect(fmt.initials("alice WONDER")).toBe("aw"));
});

describe("fmt time helpers", () => {
  it("absTime returns dash for empty", () => expect(fmt.absTime("")).toBe("—"));
  it("absDate returns dash for empty", () => expect(fmt.absDate("")).toBe("—"));
  it("relTime returns dash for empty", () => expect(fmt.relTime("")).toBe("—"));
  it("relTime renders s ago for very recent", () => {
    const iso = new Date(Date.now() - 5_000).toISOString();
    expect(fmt.relTime(iso)).toMatch(/s ago$/);
  });
});

describe("testStats", () => {
  const cells: Cell[] = [
    { run_id: "r1", status: "pass", duration_ms: 10 },
    { run_id: "r2", status: "fail", duration_ms: 20 },
    { run_id: "r3", status: "skip", duration_ms: 0 },
    { run_id: "r4", status: "pass", duration_ms: 30 },
  ] as Cell[];

  it("counts pass/fail/skip", () => {
    const s = testStats(cells);
    expect(s.pass).toBe(2);
    expect(s.fail).toBe(1);
    expect(s.skip).toBe(1);
    expect(s.total).toBe(4);
    expect(s.ran).toBe(3);
  });

  it("computes failRate from ran cells only", () => {
    const s = testStats(cells);
    expect(s.failRate).toBeCloseTo(1 / 3);
  });

  it("lastStatus is the last non-skip cell", () => {
    expect(testStats(cells).lastStatus).toBe("pass");
  });

  it("returns zeros for empty input", () => {
    const s = testStats([]);
    expect(s.total).toBe(0);
    expect(s.ran).toBe(0);
    expect(s.failRate).toBe(0);
    expect(s.lastStatus).toBe("skip");
  });
});

describe("decodeTestId", () => {
  it("decodes percent-escaped", () => expect(decodeTestId("a%2Fb")).toBe("a/b"));
  it("returns input on malformed", () => expect(decodeTestId("a%ZZ")).toBe("a%ZZ"));
});

describe("buildTestTree", () => {
  it("groups by /, ., ::, > separators", () => {
    const rows: TestRow[] = [
      { test_id: "pkg/one.TestA", test_name: "TestA", cells: [] },
      { test_id: "pkg/two.TestB > sub > leaf", test_name: "leaf", cells: [] },
    ] as TestRow[];
    const tree = buildTestTree(rows);
    expect(tree.children.length).toBeGreaterThan(0);
    expect(tree.tests.length).toBe(2);
  });

  it("returns empty branch for no rows", () => {
    const tree = buildTestTree([]);
    expect(tree.children.length).toBe(0);
    expect(tree.tests.length).toBe(0);
  });
});

describe("runCounts", () => {
  it("tallies per-run cells", () => {
    const tests: TestRow[] = [
      { test_id: "a", test_name: "a", cells: [{ run_id: "r1", status: "pass", duration_ms: 5 }] },
      { test_id: "b", test_name: "b", cells: [{ run_id: "r1", status: "fail", duration_ms: 10 }] },
    ] as TestRow[];
    const c = runCounts(tests, "r1");
    expect(c.pass).toBe(1);
    expect(c.fail).toBe(1);
    expect(c.total).toBe(2);
    expect(c.total_ms).toBe(15);
  });
});

describe("cellsByRun", () => {
  it("indexes cells by run_id", () => {
    const cells: Cell[] = [
      { run_id: "x", status: "pass", duration_ms: 1 },
      { run_id: "y", status: "fail", duration_ms: 2 },
    ] as Cell[];
    const m = cellsByRun(cells);
    expect(m.get("x")?.status).toBe("pass");
    expect(m.get("y")?.status).toBe("fail");
  });
});

describe("worstStatus", () => {
  it("fail beats pass beats skip", () => {
    expect(
      worstStatus([
        { run_id: "1", status: "pass", duration_ms: 0 },
        { run_id: "2", status: "fail", duration_ms: 0 },
      ] as Cell[]),
    ).toBe("fail");
    expect(
      worstStatus([
        { run_id: "1", status: "pass", duration_ms: 0 },
      ] as Cell[]),
    ).toBe("pass");
    expect(
      worstStatus([
        { run_id: "1", status: "skip", duration_ms: 0 },
      ] as Cell[]),
    ).toBe("skip");
    expect(worstStatus([])).toBeUndefined();
  });
});

describe("cn", () => {
  it("joins truthy", () => expect(cn("a", "b")).toBe("a b"));
  it("filters falsy", () => expect(cn("a", false, null, undefined, "b")).toBe("a b"));
  it("empty -> empty", () => expect(cn()).toBe(""));
});

// The localStorage-backed `suppression` store was replaced with a
// React Query hook (useSuppressions) backed by /api/suppressions. The
// equivalent behavior is now exercised end-to-end in
// internal/serve/suppressions_test.go.
