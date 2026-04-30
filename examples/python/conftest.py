import pytest


@pytest.fixture
def good_fixture():
    return 42


@pytest.fixture
def broken_fixture():
    raise ValueError("intentional setup failure")
