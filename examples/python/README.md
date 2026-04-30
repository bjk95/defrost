# Python integration example

A pytest project covering the cases the defrost pytest adapter parses out
of JUnit XML. The CI workflow at `.github/workflows/integration.yml`
runs `defrost exec pytest examples/python/` against this
directory and asserts on the exit code and result count.

## Test inventory

| File | Test | Outcome | Notes |
|---|---|---|---|
| `test_basics.py` | `test_pass` | pass | also exercises `<system-out>` capture (has a `print`) |
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
