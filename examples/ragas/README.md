# RAGAS example

[RAGAS](https://docs.ragas.io/) is a Python library for evaluating
Retrieval-Augmented Generation pipelines. Unlike Promptfoo, it has no CLI
and no auto-dump — `ragas.evaluate(...)` returns scores in memory and
nothing is written to disk by default. Defrost can't observe RAGAS results
without a small one-time user opt-in.

## Setup (once per project)

Drop the contents of [`conftest.py`](./conftest.py) into your repo's
top-level `conftest.py`. That's it.

The conftest monkey-patches `ragas.evaluate` (and `aevaluate`) at import
time so every call across your test suite records its result rows into a
session-scoped accumulator. `pytest_sessionfinish` writes the aggregated
JSON to `$DEFROST_RAGAS_OUT` once at the end of the run. Defrost ships
zero Python code; the snippet uses only the stdlib + pytest.

When `DEFROST_RAGAS_OUT` is unset (i.e. tests run as plain `pytest`,
without the `defrost exec` wrapper) the hook is a no-op, so the same
conftest is safe to keep checked in.

## Usage (no per-test glue)

User test files are unchanged from how they'd be written without defrost
— no defrost imports, no fixture parameters, no extra function calls:

```python
from ragas import evaluate
from ragas.metrics import faithfulness, answer_relevancy

def test_rag_scores():
    result = evaluate(dataset, metrics=[faithfulness, answer_relevancy])
    assert result["faithfulness"] > 0.7
```

Multiple tests calling `evaluate(...)` independently all contribute to
the same aggregated dump — the session-finish hook writes once at the
end, so nothing overwrites anything else.

## Why this shape

`EvaluationResult.to_pandas().to_dict(orient="records")` is built into
RAGAS — defrost ships zero Python helper modules. Each row is a dict with
all DataFrame columns flattened (input columns plus one float column per
scorer). Defrost's parser identifies score columns by excluding known
input/reference column names (`question`, `answer`, `contexts`,
`ground_truth`, `user_input`, `response`, `reference`, etc.) and emits
one OTLP gauge per remaining numeric column.

The conftest's monkey-patch trades fragility for ergonomics: if a future
RAGAS release renames `evaluate` or removes `to_pandas`, the snippet
needs an update. That cost lives in the user's repo where they can pin
their RAGAS version, rather than in the defrost binary where every user
would feel a version-skew bug.

## Metrics emitted

Per (row × scorer), one OTLP gauge with these attributes:

| Attribute | Value |
|---|---|
| `gen_ai.evaluation.name` | scorer name (e.g. `faithfulness`) |
| `gen_ai.evaluation.score.value` | float in `[0.0, 1.0]` |
| `test.case.name` | `ragas_row_<i>` (across the aggregated dump) |

RAGAS's result object carries no per-metric threshold or judge-model
reference, so `defrost.eval.threshold` and `defrost.eval.judge_model` are
absent from these metrics. Pass/fail is decided by the user's pytest
assertions, not by defrost.
