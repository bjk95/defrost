# JavaScript/TypeScript (jest) Support — Design

**Date:** 2026-04-30
**Status:** Implemented (2026-04-30). See **Errata** below for corrections to the JSON shape that were discovered during the smoke test for `examples/javascript/`. The struct names and field-mapping table in this doc reflect the original (incorrect) plan; the merged code is the source of truth.

## Errata (post-implementation)

The original design described jest's `--json --outputFile` shape using fields from a documentation snippet that turned out to describe the `testResultsProcessor` configuration callback, not the CLI output. Jest 29's actual `--json --outputFile` document differs in three places:

| What the design said | What jest 29 actually emits |
|---|---|
| `testFilePath` (file entry) | `name` |
| nested `testResults` (assertion array on a file entry) | `assertionResults` |
| `testExecError: { message, stack }` (file-level error object) | No such field. File-level errors are signaled by `assertionResults: []` + file `status: "failed"` + a populated `message` string at the file level. |
| `startAt` (per-assertion epoch ms) | No such field. `StartTime` defaults to zero `time.Time` (matching the pytest path). |

Implications carried into the shipped code:

- `parser.go` uses `name` and `assertionResults` for the JSON tags, drops the `jestExecError` struct, and treats the file-error path as `len(assertionResults) == 0 && status == "failed" && message != ""` → emit one synthetic `<rel>::<file-error>` with `Output = message`.
- `models.TestResult.StartTime` is always zero for the jest path (jest 29 emits per-file `startTime` only, not per-assertion).
- `testdata/exec-error.json` uses the file-level message convention; the other 8 fixtures use `name` / `assertionResults`.

The mapping table in "Components → `internal/javascript/jest/parser.go`" below is the original (incorrect) design. Read the merged `parser.go` for ground truth.



## Purpose

Extend `defrost` to wrap jest invocations the same way it wraps `go test` and `pytest`. After this change, a user with `defrost` and `jest` already installed (locally in `node_modules`, via `npx`, or via a package-runner script) can run `defrost exec jest tests/` (or any of the supported invocation forms) and get back parsed `models.TestResult` records — no extra setup, no plugin install, no flags they have to remember.

This builds on the adapter registry introduced in `2026-04-29-python-pytest-support-design.md`. Jest slots in as a third adapter alongside `golang` and `python/pytest`.

Out of scope for this spec: server upload, classification, retries, exit-code rewriting, summary output, frameworks other than jest (vitest, mocha, ava, jasmine, bun-test), watch mode, parent-directory walk for `package.json`, Windows path forms, parsing composite shell scripts, auto-installing third-party reporters such as `jest-junit`, capturing per-test stdout/stderr (jest's JSON has no equivalent of JUnit `system-out`).

## High-level flow

```
user shell ──► defrost exec <jest-form> [user args]
                  │
                  ├─► runner.Find(cmd) picks the jest adapter
                  │     (form (d) — `npm test` / `yarn <script>` / `pnpm test` —
                  │      reads ./package.json and checks scripts.<name> shape)
                  │
                  ├─► adapter spawns: <jest-form> [user args] \
                  │                   [--] --json --outputFile=<tempfile>
                  │
                  │   child stdout ─► defrost stdout (JSON blob; mostly empty after parse)
                  │   child stderr ─► defrost stderr (live progress output)
                  │
                  ├─► waits, captures exit code
                  ├─► reads tempfile, parses jest JSON → []models.TestResult
                  ├─► returns results + exit code to exec.HandleExecution
                  ├─► defer: deletes tempfile
                  └─► HandleExecution emits results via fmt.Printf("%+v\n", r)
                       and persists via the existing persist path
```

The `--` separator before `--json --outputFile=...` is conditional on the invocation form (see below).

## User-facing behavior

The following invocations all trigger the jest adapter (any args after `jest`/the script name are passed through):

```
defrost exec jest tests/
defrost exec npx jest tests/
defrost exec yarn jest tests/
defrost exec pnpm jest tests/
defrost exec bunx jest tests/
defrost exec ./node_modules/.bin/jest tests/
defrost exec node_modules/.bin/jest tests/

defrost exec npm test
defrost exec npm test -- tests/foo.test.js
defrost exec npm run <script-name>
defrost exec yarn test
defrost exec yarn run test
defrost exec yarn <script-name>
defrost exec pnpm test
defrost exec pnpm run <script-name>
```

For the package-runner forms (last block), defrost reads `./package.json` and accepts only when `scripts.<name>` has **strict jest shape** (see "Form (d): script shape check").

The wrapper:

1. Detects jest from the argv shape, plus a `package.json` lookup for forms (d).
2. Appends `--json --outputFile=<defrost-tempfile>` to the user's argv. Inserts a `--` separator before those flags when needed (npm/pnpm — see "Flag injection").
3. Streams the child's stderr to the wrapper's stderr verbatim (this is where jest's progress output goes when `--json` is set). Child stdout is left wired to the wrapper's stdout (jest writes the JSON blob to `--outputFile` rather than stdout when both flags are present, but anything still written to stdout passes through).
4. Waits for the child to exit and parses the JSON file the child wrote.
5. Returns each `TestResult` to `exec.HandleExecution`, which emits them and runs the existing persistence path.
6. Deletes the tempfile and returns the child's exit code.

If the user already passed `--json` or `--outputFile=<anything>`, defrost prints an error to stderr and exits 2 — defrost will not silently override an explicit user choice.

If the user passed `--watch` or `--watchAll`, defrost prints an error to stderr and exits 2. Jest in watch mode never exits, so no JSON file is ever written, so there is nothing to parse.

If the child fails to start, exits without writing the JSON file, or writes an unparseable JSON file, defrost logs to stderr and exits 1.

## Components

### `internal/javascript/jest/adapter.go`

New. Implements `runner.Adapter` for jest. The matcher dispatches on `cmd[0]`:

| `cmd[0]` | Match rule |
|---|---|
| `jest` | always match |
| `npx` | match if `cmd[1] == "jest"` (also accept `npx -y jest`, `npx --no-install jest`, etc. — skip leading flags between `npx` and `jest`) |
| `bunx` | match if `cmd[1] == "jest"` |
| `yarn` | match if `cmd[1] == "jest"`. Else if `cmd[1] == "run" && cmd[2] == "<name>"`, or `cmd[1] == "<name>"` (no `run`), check `scripts.<name>` in `./package.json`. Default `<name>` is `"test"` for `yarn test`. |
| `pnpm` | match if `cmd[1] == "jest"`. Else if `cmd[1] == "test"` or `cmd[1] == "run" && cmd[2] == "<name>"`, check `scripts.<name>`. |
| `npm` | if `cmd[1] == "test"`, check `scripts.test`. Else if `cmd[1] == "run" && cmd[2] == "<name>"`, check `scripts.<name>`. (Bare `npm jest` is not a thing.) |
| ends in `node_modules/.bin/jest` (suffix match) | always match |

The matcher is the only place in the registry where filesystem I/O happens. This is a deliberate, scoped exception: only the npm/yarn/pnpm branches read `./package.json`, and only when the command shape *could* be a script-runner. All other branches are pure argv inspection. The pytest matcher is pure argv inspection. The Go matcher is pure argv inspection. This divergence is documented here so a future reader doesn't try to "fix" it without understanding the tradeoff.

`Run` does the orchestration: collision check, tempfile, argv mutation, child spawn, parse, cleanup, exit-code propagation. Same shape as the pytest adapter, with two differences:

- Jest's `--json` sends progress to **stderr**, not stdout — so we wire `child.Stderr = os.Stderr` (live progress) and `child.Stdout = os.Stdout` (mostly silent in `--json` mode).
- For npm/pnpm forms, `Run` inserts a `--` separator before injected flags. For yarn, no separator. For all direct/binary forms, flags append normally.

### Form (d): script shape check

Reads `./package.json` (no parent walk; cwd only). Locates `scripts.<name>` where `<name>` is determined by the invocation:

- `npm test`, `yarn test`, `pnpm test` → `<name> = "test"`
- `npm run X`, `yarn run X`, `pnpm run X` → `<name> = X`
- `yarn X` (no `run`) → `<name> = X`

If the file doesn't exist, isn't valid JSON, or has no `scripts.<name>`, **no match**.

If `scripts.<name>` exists, it is tokenized with `strings.Fields` (whitespace split — sufficient because we reject anything containing shell metacharacters anyway) and checked for **strict jest shape**:

1. Strip a leading run of `KEY=value` tokens (env-var assignments). A token qualifies as `KEY=value` iff `regexp.MustCompile(\`^[A-Za-z_][A-Za-z0-9_]*=\`)` matches it.
2. The next token after env-var prefix must be exactly `jest`.
3. The remaining tokens must not contain any of: `&&`, `||`, `;`, `|`, `>`, `<`, `&`, backtick, `$(`, or `(`. (We reject composite shell commands rather than try to parse them.)

If all three checks pass → match. Otherwise → no match. When defrost rejects the script for shape reasons (it exists but isn't strict-jest-shaped), `Run` prints to stderr:

```
defrost: scripts.<name> in package.json doesn't look like a direct jest invocation; run jest via 'npx jest …' or rewrite the script
```

…and returns 2. (This message lives in `Run` rather than the matcher because the matcher just decides match/no-match. The shape failure is detected during `Matches`, which records the reason on the adapter struct and returns true; `Run` then surfaces the error. See "Adapter struct" below.)

#### Adapter struct

```go
type Adapter struct {
    // populated during Matches when form (d) is detected; used by Run to
    // decide whether to emit a "script doesn't look like jest" error.
    formD       bool
    scriptName  string
    scriptValue string
    scriptOK    bool   // true iff strict-jest-shape passed
}
```

`Matches` mutates the receiver via pointer (so the adapter is registered as `&jest.Adapter{}`, not `jest.Adapter{}`). This is a small departure from the value-receiver style of the pytest adapter; it's the simplest way to thread the script-resolution result from `Matches` into `Run` without re-reading `package.json`.

(Alternative considered: have `Matches` return false for non-jest scripts and let the user see the generic "no adapter for X" error. Rejected because the helpful error message at form-(d) script-shape failure is the whole point of supporting form (d) — without it, the user gets a confusing "no adapter for `npm`" instead of a pointer at their script.)

### Flag injection

For each form, the appended flags are:

| Form | Appended |
|---|---|
| `jest …`, `npx jest …`, `bunx jest …`, `yarn jest …`, `pnpm jest …`, `./node_modules/.bin/jest …` | `--json --outputFile=<path>` |
| `npm test`, `npm run X`, `pnpm test`, `pnpm run X` | `-- --json --outputFile=<path>` |
| `yarn test`, `yarn run X`, `yarn X` | `--json --outputFile=<path>` (yarn forwards directly without `--`) |

Injection rule for npm/pnpm: if a `--` token is already present in the user's argv, append `--json --outputFile=<path>` after the user's last token (which sits after the existing `--`). If no `--` is present, append `-- --json --outputFile=<path>` at the end. Defrost does not reorder or remove any user-provided tokens. Examples:

- `npm test` → `npm test -- --json --outputFile=<path>`
- `npm test -- tests/foo.test.js` → `npm test -- tests/foo.test.js --json --outputFile=<path>`
- `npm test tests/foo.test.js` → `npm test tests/foo.test.js -- --json --outputFile=<path>` (npm itself ignores positional args before `--`, but that's the user's mistake; defrost preserves the input verbatim)

### `internal/javascript/jest/adapter_test.go`

Table-driven tests over the matcher. All cases here are pure-argv (forms a/b/c/e plus negatives):

- `jest` → match
- `jest tests/` → match
- `npx jest tests/` → match
- `npx -y jest tests/` → match
- `yarn jest tests/` → match
- `pnpm jest tests/` → match
- `bunx jest tests/` → match
- `./node_modules/.bin/jest tests/` → match
- `node_modules/.bin/jest tests/` → match
- `go test ./...` → no match
- `pytest tests/` → no match
- `jest --json` → match (collision is enforced inside `Run`, not the matcher)
- `jest --watch` → match (watch is enforced inside `Run`, not the matcher)

For form (d), the matcher reads `./package.json`, so those cases use a per-test temp directory + `t.Chdir(dir)`. Cases:

- `package.json` with `scripts.test = "jest"` + `npm test` → match
- `package.json` with `scripts.test = "jest --config=foo.js"` + `npm test` → match
- `package.json` with `scripts.test = "NODE_ENV=test jest"` + `npm test` → match
- `package.json` with `scripts.test = "JEST_JUNIT_OUTPUT=x.xml NODE_ENV=test jest"` + `npm test` → match (multiple env-var prefix tokens)
- `package.json` with `scripts.test = "cross-env NODE_ENV=test jest"` + `npm test` → no match (cross-env wrapper rejected)
- `package.json` with `scripts.test = "jest && eslint ."` + `npm test` → no match (composite)
- `package.json` with `scripts.test = "react-scripts test"` + `npm test` → no match (no jest token in head position)
- `package.json` with `scripts.test = "vitest"` + `npm test` → no match
- `package.json` with `scripts.test = "node --experimental-vm-modules node_modules/jest/bin/jest.js"` + `npm test` → no match (jest is not the first command word)
- `package.json` missing → no match
- `package.json` invalid JSON → no match
- `package.json` with no `scripts.test` + `npm test` → no match
- `package.json` with `scripts.lint = "eslint ."` + `npm run lint` → no match
- `package.json` with `scripts.jest = "jest"` + `npm run jest` → match
- `yarn test` with `scripts.test = "jest"` → match
- `yarn jest` (direct) → match without reading `package.json`
- `pnpm run test` with `scripts.test = "jest"` → match

### `internal/javascript/jest/parser.go`

New. Parses a jest `--json --outputFile` document into `[]models.TestResult`.

Decoding shape (only fields we use):

```go
type jestDoc struct {
    TestResults    []jestFileResult `json:"testResults"`
}

type jestFileResult struct {
    TestFilePath  string           `json:"testFilePath"`
    TestExecError *jestExecError   `json:"testExecError"`
    AssertionResults []jestAssertion `json:"testResults"` // nested name reused
}

type jestExecError struct {
    Message string `json:"message"`
    Stack   string `json:"stack"`
}

type jestAssertion struct {
    Title           string   `json:"title"`
    Status          string   `json:"status"`
    AncestorTitles  []string `json:"ancestorTitles"`
    FailureMessages []string `json:"failureMessages"`
    Duration        *float64 `json:"duration"` // ms, nullable
    StartAt         *int64   `json:"startAt"`  // epoch ms, nullable
}
```

Mapping rules per assertion:

| `models.TestResult` field | Source |
|---|---|
| `Id` | `<rel-path>::<ancestors joined " > "> > <title>`, where `<rel-path>` is `testFilePath` made relative to `cwd` via `filepath.Rel`. If `Rel` returns an error, fall back to the absolute path. If `ancestorTitles` is empty, just `<rel-path>::<title>`. |
| `Ran` | `status` is `"passed"` or `"failed"` → true. Anything else (`"pending"`, `"todo"`, `"disabled"`, unknown) → false. |
| `Passed` | `status == "passed"` |
| `Duration` | `time.Duration(*duration * float64(time.Millisecond))`, zero if `duration` is nil |
| `StartTime` | `time.UnixMilli(*startAt)` if non-nil, else zero `time.Time` |
| `Output` | `strings.Join(failureMessages, "\n")` if `status == "failed"`, else empty string |

For each `jestFileResult`:

- If `assertionResults` is non-empty, emit one `TestResult` per assertion as above.
- If `assertionResults` is empty AND `testExecError` is non-nil, emit one synthetic `TestResult`:
  - `Id` = `<rel-path>::<file-error>`
  - `Ran` = true
  - `Passed` = false
  - `StartTime` = zero
  - `Duration` = 0
  - `Output` = `testExecError.Message + "\n" + testExecError.Stack` (newline-separated; empty parts dropped)
- If `assertionResults` is empty AND `testExecError` is nil, emit nothing for that file.

Top-level `numTotalTests == 0` with no `testResults` entries → empty result slice (we still propagate jest's exit code unchanged; jest itself decides whether zero-tests is an error via `--passWithNoTests`).

### `internal/javascript/jest/parser_test.go`

Table-driven tests over fixture JSON files in `internal/javascript/jest/testdata/`:

- `pass.json` — single passing assertion → `Passed=true, Ran=true`, duration parsed, StartTime parsed
- `fail.json` — single failing assertion with `failureMessages` → `Passed=false, Ran=true`, joined messages in `Output`
- `pending.json` — `status="pending"` → `Ran=false, Passed=false`
- `todo.json` — `status="todo"` → `Ran=false, Passed=false`
- `ancestors.json` — nested describe → `Id` includes `" > "` joined ancestors
- `multi-file.json` — two files, mixed pass/fail/skip → all assertions emitted, `Id`s use relative paths
- `exec-error.json` — file with `testExecError` and empty `assertionResults` → one synthetic result with file-error suffix
- `null-duration.json` — `duration: null` and `startAt: null` → `Duration=0`, `StartTime` zero
- `empty.json` — `testResults: []` → empty result slice

### `internal/javascript/jest/testdata/`

Fixture JSON files for `parser_test.go`. Each is a minimal jest output document covering one row in the parser test table. To keep the path-relativization stable across machines, fixtures use a placeholder `__CWD__` token in `testFilePath`; the parser test rewrites this to `t.TempDir()` before parsing so `filepath.Rel` produces predictable output.

### `exec.go`

Edit. One additional `Register` call:

```go
reg := runner.NewRegistry()
reg.Register(golang.Adapter{})
reg.Register(pytest.Adapter{})
reg.Register(&jest.Adapter{})  // pointer receiver — see Adapter struct above
```

No other changes to `exec.go`. Persistence flows through the existing `persistResults` call.

### `main.go`

Unchanged.

### `examples/javascript/`

New. Mirrors `examples/python/` in shape. Contains:

- `package.json` — `{"scripts": {"test": "jest"}, "devDependencies": {"jest": "^29"}}` (pinning the major; jest 29 is what gets installed by `npm i -D jest` on a fresh project today). Add `"private": true` to silence npm warnings.
- `jest.config.js` — minimal `{ testMatch: ["**/*.test.js"] }`. (Jest's defaults already match `*.test.js`, but spelling it out makes the example self-documenting.)
- `.gitignore` — `node_modules/`
- `basics.test.js` — passing test, failing test, async passing test, async failing test (4 results)
- `describe.test.js` — nested describes with one pass and one fail to exercise `ancestorTitles` (2 results)
- `each.test.js` — `test.each([[1,1,2],[2,2,4]])` with one expected to fail (3 results)
- `skip.test.js` — `test.skip("…", …)`, `test.todo("…")`, and `xit("…", …)` (3 results, all `Ran=false`)
- `README.md` — short note that this exists as defrost's jest fixture

Total: 12 expected `TestResult` lines. Adjust the test bodies to land on a clean number if 12 feels arbitrary; the CI assertion will be exact.

### `examples/typescript/`

New. Same shape as `examples/javascript/` but using TypeScript via `ts-jest`:

- `package.json` — `{"scripts": {"test": "jest"}, "devDependencies": {"jest": "^29", "ts-jest": "^29", "typescript": "^5", "@types/jest": "^29"}}`, `"private": true`.
- `jest.config.js` — `{ preset: "ts-jest", testMatch: ["**/*.test.ts"] }`.
- `tsconfig.json` — minimal `{ compilerOptions: { target: "es2020", module: "commonjs", strict: true, esModuleInterop: true } }`.
- `.gitignore` — `node_modules/`
- `basics.test.ts`, `describe.test.ts`, `each.test.ts`, `skip.test.ts` — same scenarios as `examples/javascript/`, rewritten in TS with explicit type annotations on at least one assertion to exercise the type-checked path.
- `README.md` — short note.

Same 12 expected `TestResult` lines.

### `.github/workflows/integration.yml`

Edit. Add two jobs alongside the existing `python` job: `javascript` and `typescript`. They are nearly identical, differing only in the example directory and the install steps.

```yaml
javascript:
  name: jest example (js)
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version-file: go.mod
    - uses: actions/setup-node@v4
      with:
        node-version: "20"
    - name: Install jest
      run: npm ci
      working-directory: examples/javascript
    - name: Build defrost
      run: go build -o defrost .
    - name: Run defrost against examples/javascript
      id: run
      run: |
        set +e
        cd examples/javascript
        ../../defrost exec --no-persist npm test > out.txt 2>&1
        echo "exit=$?" >> "$GITHUB_OUTPUT"
        exit 0
    - name: Assert exit code is non-zero (intentional failures)
      run: |
        code="${{ steps.run.outputs.exit }}"
        if [ "$code" -eq 0 ]; then
          echo "FAIL: expected non-zero exit"
          cat examples/javascript/out.txt
          exit 1
        fi
    - name: Assert result count matches expected
      run: |
        count=$(grep -c '^{Id:' examples/javascript/out.txt || true)
        if [ "$count" -ne 12 ]; then
          echo "FAIL: expected 12 TestResult lines, got $count"
          cat examples/javascript/out.txt
          exit 1
        fi
    - name: Upload defrost output for debugging
      if: failure()
      uses: actions/upload-artifact@v4
      with:
        name: javascript-out
        path: examples/javascript/out.txt
```

The `typescript` job is the same with `examples/typescript`, `typescript-out` artifact name, and `npm ci` picking up `ts-jest` from devDependencies. Both jobs `cd` into the example directory so `./package.json` resolves correctly for the form-(d) matcher.

Both invocations use `npm test`, exercising form (d) end-to-end. The matcher reads `examples/javascript/package.json` (or `…/typescript/`), confirms `scripts.test == "jest"` is strict-jest-shaped, and the adapter spawns `npm test -- --json --outputFile=<tempfile>`.

If we want to also exercise direct invocation, a follow-up CI step could run `../../defrost exec --no-persist npx jest`, but for v1 the form-(d) path is the more interesting integration check (it depends on the most code).

## Data flow

```
defrost exec npm test
   │
   ▼
exec.HandleExecution(cmd, opts)
   │
   ├─► runner.Find(cmd) → &jest.Adapter{} with formD=true, scriptOK=true
   │
   ▼
jest.Adapter.Run(cmd)
   │
   ├─► collision check (--json / --outputFile / --watch / --watchAll)
   ├─► os.CreateTemp("", "defrost-jest-*.json") → path
   ├─► insertSeparatorIfNeeded(cmd) + append "--json" "--outputFile=<path>"
   ├─► spawn cmd[0] with the rebuilt argv
   │      child stdout ──► os.Stdout
   │      child stderr ──► os.Stderr  (jest --json sends progress here)
   │
   ├─► child.Wait() → exitCode
   │
   ├─► parser.ParseFile(path, cwd) → []models.TestResult, err
   │
   ├─► defer os.Remove(path)
   │
   └─► return results, exitCode
       │
       ▼
exec.HandleExecution
   │
   ├─► for r := range results: fmt.Printf("%+v\n", r)
   ├─► if opts.Persist: persistResults(...)
   └─► return exitCode
```

## Error handling

| Condition | Behaviour |
|---|---|
| User passed `--json` or `--outputFile=<anything>` | `Run` detects collision, prints `defrost: jest adapter requires control of --json and --outputFile; remove those flags` to stderr, returns 2. |
| User passed `--watch` or `--watchAll` | `Run` rejects, prints `defrost: jest adapter is not compatible with --watch / --watchAll` to stderr, returns 2. |
| Form (d): `package.json` missing | Matcher returns false; `runner.Find` returns nil; `exec.go` prints generic `exec: unsupported test command` to stderr, returns 2. |
| Form (d): `package.json` invalid JSON | Same as missing — matcher returns false. |
| Form (d): `scripts.<name>` missing | Same as missing — matcher returns false. |
| Form (d): `scripts.<name>` not strict-jest-shaped | Matcher returns true (form recognized), but `scriptOK=false`; `Run` prints `defrost: scripts.<name> in package.json doesn't look like a direct jest invocation; run jest via 'npx jest …' or rewrite the script` to stderr, returns 2. |
| jest binary not found | `cmd.Start` returns error; adapter prints to stderr, returns 1. |
| jest exits non-zero (test failures) | Adapter parses JSON normally and returns jest's exit code unchanged. |
| jest exited but JSON file missing | Adapter prints to stderr, returns 1. |
| jest exited and JSON file is malformed | `encoding/json` returns an error; adapter prints it to stderr, returns 1. |
| `testExecError` set on a file (test file failed to load) | Parser emits a synthetic `TestResult` with `Id = <rel-path>::<file-error>`, `Ran=true, Passed=false`, `Output` = message + stack. Exit code is whatever jest returned (typically non-zero). |
| `numTotalTests == 0` (jest found no tests) | Empty result slice. Exit code unchanged (jest itself signals via non-zero exit unless `--passWithNoTests`). |
| I/O error during stdout/stderr piping | Adapter prints to stderr, returns 1. |

## Testing

Coverage targets only the parts with non-trivial logic:

- `internal/javascript/jest/parser_test.go` — JSON → `TestResult` mapping (table-driven, fixtures, including `testExecError` and null durations).
- `internal/javascript/jest/adapter_test.go` — matcher, including all package.json shape cases for form (d).
- `internal/runner/registry_test.go` — already exists; no changes needed.

No tests for argv mutation, child-process plumbing, separator insertion, or `exec.go`. The plumbing is small enough that the integration jobs (`examples/javascript/` + `examples/typescript/` running under CI) provide end-to-end verification, mirroring the existing pytest-side decision in `2026-04-29-python-pytest-support-design.md`.

## Non-goals

The following are explicitly *not* part of this spec:

- HTTP client, server, or upload logic.
- Database, persistence, or local cache (already provided by `internal/persist` — jest gets it for free via `exec.HandleExecution`).
- Verdict classification, retry logic, or exit-code rewriting based on test outcomes.
- Human-readable summary output.
- Support for JS/TS frameworks other than jest (vitest, mocha, ava, jasmine, bun-test, node:test).
- Auto-installing `jest`, `ts-jest`, or any plugin.
- Watch mode, interactive mode, or any other long-running jest mode.
- `package.json` parent-directory walk (cwd only).
- Parsing composite shell scripts in `scripts.<name>` (anything beyond `KEY=value … jest …`).
- Wrapper commands like `cross-env`, `concurrently`, `npm-run-all`, `node --experimental-vm-modules`.
- `yarn dlx` (yarn berry's npx equivalent — same shape as `npx jest` should work but isn't tested in v1).
- Windows path forms in `node_modules/.bin/jest` matcher.
- Capturing per-test stdout/stderr (jest's JSON has no `system-out`/`system-err` equivalent).
- Snapshot test diff output beyond what's already in `failureMessages`.
- Coverage data extraction from jest's `coverage` blob.
- Distinguishing `pending` / `todo` / `disabled` beyond what `Passed=false, Ran=false` already conveys.

These are deferred to follow-up specs once the basic jest path is in place.
