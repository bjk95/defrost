"""Hermetic smoke test for the defrost RAGAS plugin.

The set-and-forget conftest pattern monkey-patches ``ragas.evaluate``,
which means it can only fire when the real RAGAS module is importable.
RAGAS is too heavy to take as a CI dependency for a smoke test, so this
file feeds the conftest's session aggregator directly via the ``_rows``
list — same JSON shape ``EvaluationResult.to_pandas().to_dict(orient='records')``
produces. Exercises the conftest's session-finish hook end-to-end without
RAGAS installed.

When run under defrost the plugin emits one gauge metric per (row × scorer)
and the wrapper persists the run as usual. When run as plain pytest the
``DEFROST_RAGAS_OUT`` env var is absent and the hook is a no-op.
"""

from conftest import _rows


def test_writes_canned_first_row():
    _rows.append(
        {
            "user_input": "What is the capital of France?",
            "response": "Paris.",
            "retrieved_contexts": ["France's capital is Paris."],
            "reference": "Paris",
            "faithfulness": 0.92,
            "answer_relevancy": 0.88,
        }
    )
    assert True


def test_writes_canned_second_row():
    # Demonstrates the multi-test aggregation property: this row is
    # appended to the same session-scoped list, and pytest_sessionfinish
    # writes both rows in one DEFROST_RAGAS_OUT dump.
    _rows.append(
        {
            "user_input": "Who wrote Hamlet?",
            "response": "Shakespeare.",
            "retrieved_contexts": ["William Shakespeare wrote Hamlet."],
            "reference": "William Shakespeare",
            "faithfulness": 0.31,
        }
    )
    assert True
