"""Defrost dogfood task for the Inspect AI adapter.

A deterministic mock solver returns whatever the sample's metadata
``mock_answer`` field says, so each sample exercises the ``match()``
scorer with predictable pass/fail outcomes — no LLM API calls. One
sample answers correctly, one answers wrong; the wrong-answer case is
suppressed in CI to dogfood the suppression flow.
"""

from inspect_ai import Task, task
from inspect_ai.dataset import Sample
from inspect_ai.scorer import match
from inspect_ai.solver import Generate, TaskState, solver


@solver
def mock_solver():
    async def solve(state: TaskState, generate: Generate) -> TaskState:
        state.output.completion = state.metadata.get("mock_answer", "")
        return state

    return solve


@task
def capital_cities() -> Task:
    return Task(
        dataset=[
            Sample(
                input="Capital of France?",
                target="Paris",
                metadata={"mock_answer": "Paris"},
            ),
            Sample(
                input="Capital of Germany?",
                target="Berlin",
                metadata={"mock_answer": "London"},
            ),
        ],
        solver=mock_solver(),
        scorer=match(),
    )
