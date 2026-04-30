# promptfoo example

Tiny defrost-instrumented promptfoo eval used by the integration suite.

```sh
# from this directory
npx promptfoo@latest eval -c promptfooconfig.yaml   # bare promptfoo
defrost exec npx promptfoo@latest eval -c promptfooconfig.yaml  # via defrost
```

Requires `OPENAI_API_KEY` in the environment. CI uses the same key from a
gated secret; on forks the job is skipped.
