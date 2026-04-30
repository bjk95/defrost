# promptfoo example

Tiny defrost-instrumented promptfoo eval used by the integration suite.
Uses a deterministic local JavaScript provider (`provider.js`) instead
of an LLM, so CI can dogfood the full adapter pipeline without API
credits.

```sh
# from this directory
npx promptfoo@latest eval -c promptfooconfig.yaml   # bare promptfoo
defrost exec promptfoo eval -c promptfooconfig.yaml  # via defrost
```

To run against a real LLM instead, swap the `providers:` block in
`promptfooconfig.yaml` to e.g. `openai:gpt-4o-mini` and set
`OPENAI_API_KEY` in your environment.
