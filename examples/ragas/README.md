# RAGAS example

[RAGAS](https://docs.ragas.io/) is a Python library for evaluating
Retrieval-Augmented Generation pipelines. Unlike Promptfoo, it has no CLI
and no auto-dump — `ragas.evaluate(...)` returns scores in memory and
nothing is written to disk by default. Defrost can't observe RAGAS results
without a single-line user opt-in.

## Usage

```python
from ragas import evaluate
from ragas.metrics import faithfulness, answer_relevancy
import defrost_ragas

def test_rag_scores():
    result = evaluate(dataset, metrics=[faithfulness, answer_relevancy])
    defrost_ragas.write_results(result)
    assert result["faithfulness"] > 0.7
```

When defrost wraps the test run (`defrost exec pytest examples/ragas/`),
`defrost_ragas.write_results` serialises the evaluation result to a
defrost-controlled tempfile that the RAGAS plugin reads in teardown. When
the same test runs outside defrost, the helper sees no
`DEFROST_RAGAS_OUT` env var and is a no-op — so test files stay portable.

## How the helper is shipped

`defrost_ragas.py` is embedded in the defrost binary and dropped into a
tempdir on `PYTHONPATH` for the duration of the test run. Users do not
copy or `pip install` anything; they only need to add the `import
defrost_ragas` line and the `write_results(...)` call.

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
