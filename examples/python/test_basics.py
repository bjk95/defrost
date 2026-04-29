def test_pass():
    print("captured stdout from test_pass")
    assert 1 == 1


def test_fail():
    assert 1 == 2


def test_raises():
    raise RuntimeError("intentional non-assertion failure")
