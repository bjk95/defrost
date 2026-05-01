# defrost vitest example (JavaScript)

This directory exists as a fixture for defrost's CI integration tests.
It runs vitest 3+ with intentional passing and failing tests so the
`vitest-js` job can verify that `defrost exec` records the expected
12 `TestResult` lines and propagates a non-zero exit code.

To run locally:

```sh
npm ci
defrost exec --no-persist npm test
```
