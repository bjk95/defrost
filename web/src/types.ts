export type Status = "pass" | "fail" | "skip" | string;

export interface RunSummary {
  run_id: string;
  ts: string;
  commit?: string;
  parent?: string;
  branch?: string;
  pr?: number;
  author_email?: string;
  author_name?: string;
  cmd?: string[];
  os?: string;
  arch?: string;
}

export interface Cell {
  run_id: string;
  status: Status;
  duration_ms: number;
}

export interface TestRow {
  test_id: string;
  test_name: string;
  cells: Cell[];
}

export interface GridResponse {
  runs: RunSummary[];
  tests: TestRow[];
}

export interface Entry {
  schema: number;
  test_id: string;
  test_name: string;
  run_id: string;
  ts: string;
  ran: boolean;
  passed: boolean;
  status: string;
  duration_ms: number;
  output?: string;
}

export interface RunRecord {
  schema: number;
  run_id: string;
  commit?: string;
  parent?: string;
  branch?: string;
  pr?: number;
  author_email?: string;
  author_name?: string;
  dirty: boolean;
  dirty_hash?: string;
  cmd?: string[];
  cmd_hash?: string;
  go_version?: string;
  os?: string;
  arch?: string;
  ts: string;
}

export interface TestRunDetail {
  test: Entry;
  run: RunRecord;
}

export interface SuppressionsResponse {
  test_ids: string[];
}
