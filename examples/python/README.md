# Python integration example

A pytest project covering the cases the defrost pytest adapter parses out
of JUnit XML. The CI workflow at `.github/workflows/integration.yml`
runs `defrost exec pytest examples/python/` against this
directory and asserts on the exit code and result count.

## Test inventory

| File | Test | Outcome | Notes |
|---|---|---|---|
| `test_basics.py` | `test_pass` | pass | exercises `<system-out>` capture (has a `print`) AND the OTel logs signal (calls `logging.getLogger(...).info(...)`) |
| `test_basics.py` | `test_fail` | fail | plain `AssertionError` |
| `test_basics.py` | `test_raises` | fail | non-`AssertionError` exception in call phase |
| `test_skip.py` | `test_skip_decorator` | skip | `@pytest.mark.skip` |
| `test_skip.py` | `test_skipif_true` | skip | `@pytest.mark.skipif(True, ...)` |
| `test_parametrize.py` | `test_squared[1-1]` | pass | parametrize expands to 3 testcases |
| `test_parametrize.py` | `test_squared[2-4]` | pass | |
| `test_parametrize.py` | `test_squared[3-9]` | pass | |
| `test_fixtures.py` | `test_uses_fixture` | pass | consumes `good_fixture` from `conftest.py` |
| `test_fixtures.py` | `test_with_broken_fixture` | error | `broken_fixture` raises during setup → JUnit `<error>` |

10 testcases total: 5 pass, 2 fail, 1 error, 2 skip. pytest exits 1
because of the failing/erroring tests, so defrost exits 1 too.

## OTel logs integration

`conftest.py` wires Python's stdlib `logging` to an OTel OTLP/HTTP log
exporter when `defrost exec` has set `OTEL_EXPORTER_OTLP_ENDPOINT` in
the environment. `test_pass` emits a log record so the dogfood CI run
ends up with at least one entry in the third OTel signal — verified by
the workflow's "Assert OTel logs were captured" step.

When the example runs standalone (no `defrost exec`), the OTel setup
short-circuits on the missing env var and stdlib logging behaves
exactly as it would in any other pytest project. The `try/except
ImportError` guard around the OTel imports also lets the suite run
fine if the `opentelemetry-*` packages aren't installed.
