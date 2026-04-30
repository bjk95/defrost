import pytest


@pytest.mark.parametrize("n,expected", [(1, 1), (2, 4), (3, 9)])
def test_squared(n, expected):
    assert n * n == expected
