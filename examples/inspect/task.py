"""Defrost dogfood for the Inspect AI adapter.

Uses a deterministic mock solver that returns whatever the sample's metadata
says, plus a custom numeric scorer (`numeric_match`) so each scored sample
emits an `eval.numeric_match` metric. Inspect's built-in `match()` returns
Letter values (`"C"`/`"I"`) which the v1 adapter skips with a warning -- the
custom scorer here keeps the dogfood exercising the metric path end-to-end.
"""

from inspect_ai import Task, task
from inspect_ai.dataset import Sample
from inspect_ai.scorer import Score, Target, mean, scorer, stderr
from inspect_ai.solver import Generate, TaskState, solver


@solver
def mock_solver():
    """Return the canned answer carried in each sample's metadata."""

    async def solve(state: TaskState, generate: Generate) -> TaskState:
        state.output.completion = state.metadata.get("mock_answer", "")
        return state

    return solve


@scorer(metrics=[mean(), stderr()])
def numeric_match():
    """Case-insensitive exact-match returning 1.0 / 0.0 (numeric)."""

    async def score(state: TaskState, target: Target) -> Score:
        completion = state.output.completion.strip().lower()
        expected = target.text.strip().lower()
        if completion == expected:
            return Score(value=1.0, answer=state.output.completion, explanation="match")
        return Score(value=0.0, answer=state.output.completion, explanation="mismatch")

    return score


@task
def capital_cities():
    return Task(
        dataset=[
            # passes: mock answer matches target.
            Sample(
                id="france-pass",
                input="Capital of France?",
                target="Paris",
                metadata={"mock_answer": "Paris"},
            ),
            # passes: case-insensitive exact match.
            Sample(
                id="italy-pass",
                input="Capital of Italy?",
                target="Rome",
                metadata={"mock_answer": "rome"},
            ),
            # intentional fail: mock answer diverges from target.
            Sample(
                id="germany-fail",
                input="Capital of Germany?",
                target="Berlin",
                metadata={"mock_answer": "London"},
            ),
            # intentional fail: empty answer.
            Sample(
                id="empty-fail",
                input="Capital of Spain?",
                target="Madrid",
                metadata={"mock_answer": ""},
            ),
        ],
        solver=mock_solver(),
        scorer=numeric_match(),
    )
