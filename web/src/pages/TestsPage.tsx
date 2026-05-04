import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getTests } from "@/api";
import {
  buildTestTree,
  fmt,
  testStats,
  useSuppressions,
  type TreeBranch,
  type TreeNode,
} from "@/lib/utils";
import type { Cell, RunSummary, TestRow } from "@/types";
import {
  CountsBar,
  GroupHistoryStrip,
  HistoryStrip,
  STRIP_CELL,
  STRIP_GAP,
  StatusPill,
  stripWidth,
} from "@/components/Primitives";
import { Icon } from "@/components/Icons";
import { SearchInput, Segmented } from "@/components/Controls";
import { TestsEmpty } from "@/components/EmptyStates";

type StatusFilter = "all" | "failing" | "flaky";
type WindowSize = "10" | "20" | "50";

export function TestsPage() {
  const navigate = useNavigate();
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["tests"],
    queryFn: getTests,
  });

  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [windowSize, setWindowSize] = useState<WindowSize>("20");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  if (isLoading) {
    return <p style={{ color: "var(--fg-muted)", fontSize: 13 }}>loading…</p>;
  }
  if (isError) {
    return (
      <p style={{ color: "var(--danger)", fontSize: 13 }}>
        failed: {(error as Error).message}
      </p>
    );
  }
  if (!data) return null;
  if (data.runs.length === 0) return <TestsEmpty />;

  return (
    <TestsPageInner
      runs={data.runs}
      tests={data.tests}
      search={search}
      onSearch={setSearch}
      statusFilter={statusFilter}
      onStatusFilter={setStatusFilter}
      windowSize={windowSize}
      onWindowSize={setWindowSize}
      collapsed={collapsed}
      onCollapse={setCollapsed}
      onOpenTest={(tid) => navigate(`/test?id=${encodeURIComponent(tid)}`)}
    />
  );
}

function TestsPageInner({
  runs,
  tests,
  search,
  onSearch,
  statusFilter,
  onStatusFilter,
  windowSize,
  onWindowSize,
  collapsed,
  onCollapse,
  onOpenTest,
}: {
  runs: RunSummary[];
  tests: TestRow[];
  search: string;
  onSearch: (s: string) => void;
  statusFilter: StatusFilter;
  onStatusFilter: (s: StatusFilter) => void;
  windowSize: WindowSize;
  onWindowSize: (s: WindowSize) => void;
  collapsed: Record<string, boolean>;
  onCollapse: (next: Record<string, boolean>) => void;
  onOpenTest: (testId: string) => void;
}) {
  // Subscribe to suppressions ONCE for the whole page. Per-row hooks
  // would instantiate a query observer + two mutation hooks for every
  // leaf — unnecessary scaling on big test trees. Pass the resolved
  // Set down so leaves are pure computation.
  const suppressedSet = useSuppressions().set;
  const visibleRuns = useMemo(
    () => runs.slice(0, parseInt(windowSize, 10)),
    [runs, windowSize],
  );
  const visibleRunIds = useMemo(
    () => new Set(visibleRuns.map((r) => r.run_id)),
    [visibleRuns],
  );

  const filtered = useMemo(() => {
    return tests
      .map((t) => ({
        ...t,
        cells: t.cells.filter((c) => visibleRunIds.has(c.run_id)),
      }))
      .filter((t) => {
        if (search) {
          const hay = (t.test_id + " " + t.test_name).toLowerCase();
          if (!hay.includes(search.toLowerCase())) return false;
        }
        const stats = testStats(t.cells);
        if (statusFilter === "failing" && stats.lastStatus !== "fail") return false;
        if (statusFilter === "flaky") {
          if (stats.fail === 0 || stats.pass === 0) return false;
        }
        return t.cells.length > 0;
      });
  }, [tests, search, statusFilter, visibleRunIds]);

  const tree = useMemo(() => buildTestTree(filtered), [filtered]);

  // Fixed pixel width for the run-status column. Computing it once at
  // the page level (instead of letting each row's `auto` column size
  // itself) is what keeps the colored squares right-aligned across
  // every row. Grid columns sized "auto" are content-sized, and since
  // each row is its own grid, content-sized columns end up at
  // different x-positions per row.
  const runStripWidth = stripWidth(visibleRuns.length);
  const gridColumns = `minmax(0,1fr) 80px ${runStripWidth}px`;

  // Zero right padding on every row so the run-status column's
  // rightmost cell sits at the right edge of <main>'s content area
  // (the visible "page right edge" the user sees, which is inside
  // <main>'s 24px padding). The 8px row gutter we keep on
  // left/top/bottom would otherwise leave a small gap there.
  const ROW_PAD_RIGHT = 0;

  const totalStats = useMemo(() => {
    let pass = 0, fail = 0, skip = 0, total = 0;
    for (const t of filtered) {
      for (const c of t.cells) {
        total++;
        if (c.status === "pass") pass++;
        else if (c.status === "fail") fail++;
        else skip++;
      }
    }
    return { pass, fail, skip, total };
  }, [filtered]);

  const toggle = (path: string) =>
    onCollapse({ ...collapsed, [path]: !collapsed[path] });

  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 12,
          flexWrap: "wrap",
          paddingBottom: 16,
          marginBottom: 20,
          borderBottom: "1px solid var(--border)",
        }}
      >
        <SearchInput value={search} onChange={onSearch} placeholder="Filter tests…" />
        <Segmented<StatusFilter>
          value={statusFilter}
          onChange={onStatusFilter}
          options={[
            { value: "all", label: "All" },
            { value: "failing", label: "Failing" },
            { value: "flaky", label: "Flaky" },
          ]}
        />
        <Segmented<WindowSize>
          value={windowSize}
          onChange={onWindowSize}
          options={[
            { value: "10", label: "10 runs" },
            { value: "20", label: "20" },
            { value: "50", label: "50" },
          ]}
        />
        <div style={{ flex: 1 }} />
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            fontSize: 12,
            color: "var(--fg-muted)",
          }}
        >
          <CountsBar counts={totalStats} total={totalStats.total} width={120} />
          <span style={{ fontFamily: "var(--font-mono)" }}>
            <span style={{ color: "var(--success)" }}>{totalStats.pass}</span>
            <span style={{ margin: "0 4px", color: "var(--fg-subtle)" }}>·</span>
            <span style={{ color: "var(--danger)" }}>{totalStats.fail}</span>
            {totalStats.skip > 0 && (
              <>
                <span style={{ margin: "0 4px", color: "var(--fg-subtle)" }}>·</span>
                <span style={{ color: "var(--fg-muted)" }}>{totalStats.skip} skip</span>
              </>
            )}
          </span>
        </div>
      </div>

      <div
        style={{
          display: "grid",
          gridTemplateColumns: gridColumns,
          gap: 16,
          alignItems: "center",
          fontSize: 11,
          fontWeight: 500,
          color: "var(--fg-muted)",
          letterSpacing: 0.06,
          textTransform: "uppercase",
          paddingBottom: 8,
          paddingRight: ROW_PAD_RIGHT,
          marginBottom: 4,
          borderBottom: "1px solid var(--border)",
        }}
      >
        <span>Test</span>
        <span style={{ textAlign: "right" }}>Last</span>
        <span style={{ textAlign: "right" }}>← older · newer →</span>
      </div>

      {tree.children.length === 0 && (
        <div
          style={{
            padding: "48px 0",
            textAlign: "center",
            color: "var(--fg-muted)",
            fontSize: 13,
          }}
        >
          No tests match these filters.
        </div>
      )}

      {tree.children.map((node) => (
        <TreeNodeView
          key={nodeKey(node)}
          node={node}
          runs={visibleRuns}
          collapsed={collapsed}
          onToggle={toggle}
          onOpenTest={onOpenTest}
          suppressedSet={suppressedSet}
          gridColumns={gridColumns}
          padRight={ROW_PAD_RIGHT}
        />
      ))}
    </div>
  );
}

const ROW_INDENT_STEP = 16;
const ROW_BASE_INDENT = 8;

function TreeNodeView({
  node,
  runs,
  collapsed,
  onToggle,
  onOpenTest,
  suppressedSet,
  gridColumns,
  padRight,
}: {
  node: TreeNode;
  runs: RunSummary[];
  collapsed: Record<string, boolean>;
  onToggle: (path: string) => void;
  onOpenTest: (testId: string) => void;
  suppressedSet: Set<string>;
  gridColumns: string;
  padRight: number;
}) {
  if (node.kind === "leaf") {
    return (
      <TestRowView
        test={node.test}
        leafName={node.name}
        runs={runs}
        depth={node.depth}
        onClick={() => onOpenTest(node.test.test_id)}
        isSuppressed={suppressedSet.has(node.test.test_id)}
        gridColumns={gridColumns}
        padRight={padRight}
      />
    );
  }
  const isCollapsed = !!collapsed[node.path];
  const stats = nodeStats(node);
  return (
    <div>
      <BranchHeader
        node={node}
        stats={stats}
        runs={runs}
        collapsed={isCollapsed}
        onToggle={() => onToggle(node.path)}
        gridColumns={gridColumns}
        padRight={padRight}
      />
      {!isCollapsed &&
        node.children.map((c) => (
          <TreeNodeView
            key={nodeKey(c)}
            node={c}
            runs={runs}
            collapsed={collapsed}
            onToggle={onToggle}
            onOpenTest={onOpenTest}
            suppressedSet={suppressedSet}
            gridColumns={gridColumns}
            padRight={padRight}
          />
        ))}
    </div>
  );
}

function nodeStats(node: TreeBranch) {
  return testStats(node.tests.flatMap((t) => t.cells));
}

// Tree paths can collide when test_ids share segments after splitting on
// `/` `.` `::` ` > ` (e.g. `a/b` and `a.b` both yield `a → b`), and a leaf
// can collide with a sibling branch of the same name (`foo` next to
// `foo/bar`). Use kind+test_id for leaves (test_id is unique per API) and
// kind+path for branches.
function nodeKey(node: TreeNode): string {
  return node.kind === "leaf"
    ? "leaf:" + node.test.test_id
    : "branch:" + node.path;
}

function BranchHeader({
  node,
  stats,
  runs,
  collapsed,
  onToggle,
  gridColumns,
  padRight,
}: {
  node: TreeBranch;
  stats: { pass: number; fail: number; skip: number; total: number };
  runs: RunSummary[];
  collapsed: boolean;
  onToggle: () => void;
  gridColumns: string;
  padRight: number;
}) {
  const indent = ROW_BASE_INDENT + node.depth * ROW_INDENT_STEP;
  const isTop = node.depth === 0;
  return (
    <div
      onClick={onToggle}
      style={{
        display: "grid",
        gridTemplateColumns: gridColumns,
        gap: 16,
        alignItems: "center",
        padding: `${isTop ? 10 : 6}px ${padRight}px ${isTop ? 10 : 6}px ${indent}px`,
        background: isTop ? "var(--bg-subtle)" : "transparent",
        borderTop: isTop ? "1px solid var(--border)" : "none",
        borderBottom: isTop ? "1px solid var(--border)" : "1px dashed var(--border)",
        cursor: "pointer",
        userSelect: "none",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 6, minWidth: 0 }}>
        <span
          style={{
            color: "var(--fg-muted)",
            display: "inline-flex",
            transition: "transform var(--dur-fast)",
            transform: collapsed ? "rotate(-90deg)" : "none",
          }}
        >
          <Icon.ChevronDown />
        </span>
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: isTop ? 13 : 12,
            fontWeight: isTop ? 500 : 400,
            color: isTop ? "var(--fg)" : "var(--fg-muted)",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {node.name}
        </span>
        <span
          style={{
            fontSize: 11,
            color: "var(--fg-subtle)",
            marginLeft: 6,
            flex: "0 0 auto",
          }}
        >
          {node.tests.length}
        </span>
      </div>
      <div style={{ display: "flex", justifyContent: "flex-end" }}>
        <CountsBar
          counts={stats}
          total={stats.total}
          width={64}
          height={isTop ? 4 : 3}
        />
      </div>
      <div style={{ display: "flex", justifyContent: "flex-end" }}>
        <GroupHistoryStrip
          runs={runs}
          cells={collectCells(node)}
        />
      </div>
    </div>
  );
}

function collectCells(node: TreeBranch): Cell[] {
  return node.tests.flatMap((t) => t.cells);
}

function TestRowView({
  test,
  leafName,
  runs,
  depth,
  onClick,
  isSuppressed,
  gridColumns,
  padRight,
}: {
  test: TestRow;
  leafName: string;
  runs: RunSummary[];
  depth: number;
  onClick: () => void;
  isSuppressed: boolean;
  gridColumns: string;
  padRight: number;
}) {
  const [hover, setHover] = useState(false);
  const stats = testStats(test.cells);
  const indent = ROW_BASE_INDENT + depth * ROW_INDENT_STEP;
  return (
    <div
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: "grid",
        gridTemplateColumns: gridColumns,
        gap: 16,
        alignItems: "center",
        padding: `7px ${padRight}px 7px ${indent}px`,
        cursor: "pointer",
        background: hover ? "var(--bg-subtle)" : "transparent",
        borderBottom: "1px solid var(--border)",
        transition: "background var(--dur-fast) var(--ease-out)",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>
        <span style={{ color: "var(--fg-subtle)", flex: "0 0 auto" }}>
          <Icon.ChevronRight />
        </span>
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 12.5,
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
            color: isSuppressed ? "var(--fg-muted)" : "var(--fg)",
            textDecoration: isSuppressed ? "line-through" : "none",
          }}
        >
          {leafName}
        </span>
        {isSuppressed && (
          <span
            title="Suppressed — failures don't break the build"
            style={{
              display: "inline-flex",
              alignItems: "center",
              color: "var(--fg-muted)",
              flex: "0 0 auto",
            }}
          >
            <Icon.EyeOff />
          </span>
        )}
        <span style={{ fontSize: 11, color: "var(--fg-muted)", fontFamily: "var(--font-mono)" }}>
          · {fmt.duration(stats.p50)} p50
        </span>
      </div>
      <div style={{ display: "flex", justifyContent: "flex-end" }}>
        <StatusPill
          status={isSuppressed ? "suppressed" : stats.lastStatus}
          size="xs"
        />
      </div>
      <div style={{ display: "flex", justifyContent: "flex-end" }}>
        <HistoryStrip
          row={test}
          runs={runs}
          cellSize={STRIP_CELL}
          gap={STRIP_GAP}
          onCellClick={() => onClick()}
        />
      </div>
    </div>
  );
}
