---
title: 'Recording test results'
---

Wrap any supported test command with `defrost exec`. defrost selects an
adapter based on the command, parses the runner's output, and records
one span per test.

```sh
defrost exec <test-command> [args...]
```

defrost has built-in adapters for `go test`, `pytest`, `jest`, `vitest`,
[Inspect AI](https://inspect.aisi.org.uk/), and
[PromptFoo](https://www.promptfoo.dev/). Anything else can still be
recorded — see [Other runners](#other-runners) below.

## Go

```sh
defrost exec go test ./...
defrost exec go test -run TestFoo -count=3 ./internal/foo
```

Test IDs use the form `<import-path>.<TestName>`, e.g.
`github.com/bjk95/defrost.TestExec`. Subtests use Go's `/` separator:
`github.com/x/p.TestParent/sub_case`.

The adapter consumes Go's `-json` output, so any flags valid for
`go test` work.

A working example lives in
[`examples/golang/`](https://github.com/bjk95/defrost/tree/main/examples/golang).

## pytest

```sh
defrost exec pytest tests/
defrost exec pytest -k "evals" tests/
```

Test IDs match pytest's nodeids: `tests/test_basics.py::test_pass`,
`tests/test_basics.py::TestClass::test_method`,
`tests/test_basics.py::test_squared[2-4]` (parametrize variants
included).

The adapter consumes JUnit XML produced by pytest. defrost configures
this for you — you do not need a `--junitxml=` flag.

A working example lives in
[`examples/python/`](https://github.com/bjk95/defrost/tree/main/examples/python).

## Jest

```sh
defrost exec npm test
defrost exec -- npx jest --runInBand
```

Test IDs use the form `<file> > <describe...> > <test name>`, e.g.
`src/foo.test.ts > Foo > squares input`.

The adapter uses Jest's reporter API. Any Jest config the project
already has is respected.

Working examples live in
[`examples/javascript/`](https://github.com/bjk95/defrost/tree/main/examples/javascript)
and
[`examples/typescript/`](https://github.com/bjk95/defrost/tree/main/examples/typescript).

## Vitest

```sh
defrost exec npx vitest run
```

Test IDs follow the same shape as Jest:
`<file> > <describe...> > <test name>`.

Working examples live in
[`examples/vitest-js/`](https://github.com/bjk95/defrost/tree/main/examples/vitest-js)
and
[`examples/vitest-ts/`](https://github.com/bjk95/defrost/tree/main/examples/vitest-ts).

## Other runners

If your runner already speaks OTel, run it under `defrost exec` and its
spans will land on the data branch as-is:

```sh
defrost exec <your-eval-or-test-command>
```

Anything written to `OTEL_EXPORTER_OTLP_ENDPOINT` is captured. defrost
sets that env var to its embedded receiver before spawning the child —
see [OTel as ingestion](../../concepts/otel-as-ingestion/).

If your runner does **not** speak OTel and isn't supported by a
built-in adapter, the run will still be wrapped (you'll see a
`defrost.run` span with no test children) but you won't get per-test
results. Open an issue requesting an adapter, or use the
[recording-evals workflow](../recording-evals/) to push metrics
directly from your code.

## Common flags

All adapters share these `defrost exec` flags:

- `--no-persist` — run, but don't write to the data branch. Useful for
  iterating locally.
- `--dev` / `-d` — write to a local scratch dir instead of committing.
- `--repo-dir <path>` — point at a different repo.

See the [`defrost exec` reference](../../reference/cli/exec/) for the
full flag list and exit-code rules.
