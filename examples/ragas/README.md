# RAGAS example

[RAGAS](https://docs.ragas.io/) is a Python library for evaluating
Retrieval-Augmented Generation pipelines. Unlike Promptfoo, it has no CLI
and no auto-dump — `ragas.evaluate(...)` returns scores in memory and
nothing is written to disk by default. Defrost can't observe RAGAS results
without a small user opt-in.

## Usage

Add three lines after `ragas.evaluate(...)` in your test:

```python
import os
from ragas import evaluate
from ragas.metrics import faithfulness, answer_relevancy

def test_rag_scores():
    result = evaluate(dataset, metrics=[faithfulness, answer_relevancy])
    if path := os.environ.get("DEFROST_RAGAS_OUT"):
        result.to_pandas().to_json(path, orient="records")
    assert result["faithfulness"] > 0.7
```

`DEFROST_RAGAS_OUT` is only set when defrost wraps the test run, so the
`to_json` call is a no-op under plain `pytest`. The same test file runs
cleanly inside and outside defrost — no defrost-specific imports, no
helper module to install or vendor.

## Why this shape

`EvaluationResult.to_pandas().to_json(path, orient="records")` is built
into RAGAS — defrost ships zero Python code. The output is a JSON array
where each element is a dataset row with all DataFrame columns flattened
(input columns plus one float column per scorer). Defrost's parser
identifies score columns by excluding known input/reference column names
(`question`, `answer`, `contexts`, `ground_truth`, `user_input`,
`response`, `reference`, etc.) and emits one OTLP gauge per remaining
numeric column.

## Metrics emitted

Per (row × scorer), one OTLP gauge with these attributes:

| Attribute | Value |
|---|---|
| `gen_ai.evaluation.name` | scorer name (e.g. `faithfulness`) |
| `gen_ai.evaluation.score.value` | float in `[0.0, 1.0]` |
| `test.case.name` | `ragas_row_<i>` |

RAGAS's result object carries no per-metric threshold or judge-model
reference, so `defrost.eval.threshold` and `defrost.eval.judge_model` are
absent from these metrics. Pass/fail is decided by the user's pytest
assertions, not by defrost.
