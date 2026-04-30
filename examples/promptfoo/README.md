# promptfoo example

Defrost-instrumented promptfoo eval that dogfoods the adapter against
**21 deterministic assertion types** spanning string/pattern matching
(`contains`, `not-contains`, `equals`, `regex`, `icontains`,
`icontains-all`, `icontains-any`, `contains-all`, `contains-any`,
`starts-with`, `levenshtein`), format validation (`is-json`, `is-xml`,
`is-html`, `contains-json`), NLP metrics (`rouge-n`, `bleu`, `gleu`),
code evaluation (`javascript`), and metadata (`word-count`, `latency`).
Each test case exercises one assertion; the mix is intentionally 20 pass
+ 10 fail + 0 skip = 30 total results.

The mock provider in `provider.js` returns whatever each test case's
`vars.answer` says, so the eval is fully deterministic and runs in CI
without any LLM API calls.

```sh
# from this directory
npx promptfoo@latest eval -c promptfooconfig.yaml   # bare promptfoo
defrost exec promptfoo eval -c promptfooconfig.yaml  # via defrost
```

To run against a real LLM instead, swap the `providers:` block in
`promptfooconfig.yaml` to e.g. `openai:gpt-4o-mini` and set
`OPENAI_API_KEY` in your environment.
