# TypeScript jest example

Used by defrost's integration CI job to verify the jest adapter against
TypeScript test files via `ts-jest`.

12 assertions total: 4 in `basics.test.ts`, 2 in `describe.test.ts`, 3 in
`each.test.ts`, 3 in `skip.test.ts`. Several are intentional failures.

To run locally:

    cd examples/typescript
    npm install
    ../../defrost exec npm test
