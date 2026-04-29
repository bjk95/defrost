import pytest


@pytest.mark.skip(reason="demo: unconditional skip")
def test_skip_decorator():
    pass


@pytest.mark.skipif(True, reason="demo: skipif true")
def test_skipif_true():
    pass
