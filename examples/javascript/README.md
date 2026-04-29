# JavaScript jest example

Used by defrost's integration CI job to verify the jest adapter end-to-end.

12 assertions total: 4 in `basics.test.js`, 2 in `describe.test.js`, 3 in
`each.test.js`, 3 in `skip.test.js`. Several are intentional failures.

To run locally:

    cd examples/javascript
    npm install
    ../../defrost exec --no-persist npm test
