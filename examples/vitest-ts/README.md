# defrost vitest example (TypeScript)

This directory exists as a fixture for defrost's CI integration tests.
It runs vitest 3+ on TypeScript sources (vitest handles TS natively
via vite — no ts-jest analogue needed) with intentional passing and
failing tests so the `vitest-ts` job can verify that `defrost exec`
records the expected 12 `TestResult` lines and propagates a non-zero
exit code.

To run locally:

```sh
npm ci
defrost exec --no-persist npm test
```
