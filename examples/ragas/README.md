# RAGAS example

[RAGAS](https://docs.ragas.io/) is a Python library for evaluating
Retrieval-Augmented Generation pipelines. Unlike Promptfoo, it has no CLI
and no auto-dump — `ragas.evaluate(...)` returns scores in memory and
nothing is written to disk by default. Defrost can't observe RAGAS results
without a small one-time user opt-in.

## Setup (once per project)

Drop the contents of [`conftest.py`](./conftest.py) into your test
directory's `conftest.py`. It exposes a `ragas_rows` fixture and registers
a `pytest_sessionfinish` hook that writes the aggregated rows to
`$DEFROST_RAGAS_OUT` at the end of the run. Defrost ships zero Python
code; the snippet uses only the stdlib + pytest.

When `DEFROST_RAGAS_OUT` is unset (i.e. tests run as plain `pytest`,
without the `defrost exec` wrapper) the hook is a no-op, so the same
conftest is safe to keep checked in.

## Usage (per test)

```python
from ragas import evaluate
from ragas.metrics import faithfulness, answer_relevancy

def test_rag_scores(ragas_rows):
    result = evaluate(dataset, metrics=[faithfulness, answer_relevancy])
    ragas_rows.extend(result.to_pandas().to_dict(orient="records"))
    assert result["faithfulness"] > 0.7
```

The single `ragas_rows.extend(...)` line is the only test-level glue.
Multiple tests call `evaluate(...)` independently and contribute to the
same aggregated dump — the session-finish hook writes once at the end, so
nothing overwrites anything else.

## Why this shape

`EvaluationResult.to_pandas().to_dict(orient="records")` is built into
RAGAS — defrost ships zero Python helper modules. Each row is a dict with
all DataFrame columns flattened (input columns plus one float column per
scorer). Defrost's parser identifies score columns by excluding known
input/reference column names (`question`, `answer`, `contexts`,
`ground_truth`, `user_input`, `response`, `reference`, etc.) and emits one
OTLP gauge per remaining numeric column.

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
