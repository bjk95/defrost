# promptfoo example

Defrost-instrumented promptfoo eval that dogfoods the adapter against a
broad sample of assertion types: `contains`, `not-contains`, `equals`,
`regex`, `is-json`, `levenshtein`, and `javascript`. Each test case
exercises one assertion; the mix is intentionally 7 pass + 5 fail + 0
skip = 12 total results.

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
