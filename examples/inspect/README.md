# inspect-ai example

Defrost-instrumented Inspect AI eval that dogfoods the adapter. Uses a
deterministic mock solver (`mock_solver` in `task.py`) that reads each
sample's `metadata.mock_answer` and reports it as the model output, so the
eval runs in CI with predictable pass/fail outcomes and **no LLM API calls**.

The dogfood ships 4 samples (2 pass, 2 intentional fail) all scored by
Inspect's built-in `match()` scorer.

```sh
# from this directory
pip install inspect-ai
inspect eval task.py                  # bare inspect
defrost exec inspect eval task.py     # via defrost
```

To run against a real LLM, drop `mock_solver()` from the `Task` definition,
add a real `Solver` (or omit `solver=` to use Inspect's default `generate()`),
and pass `--model openai/gpt-4o` (or similar) on the command line.
