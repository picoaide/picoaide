import sys
import os

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from core.executor import check_whitelist


def test_check_whitelist_empty():
    assert check_whitelist("/any/path", []) is False
    assert check_whitelist("/any/path", None) is False


def test_check_whitelist_in_list():
    whitelist = ["/home/user/docs", "/tmp/workspace"]
    assert check_whitelist("/home/user/docs/file.txt", whitelist) is True
    assert check_whitelist("/home/user/docs/sub/file.txt", whitelist) is True
    assert check_whitelist("/tmp/workspace", whitelist) is True


def test_check_whitelist_not_in_list():
    whitelist = ["/home/user/docs"]
    assert check_whitelist("/home/user/other/file.txt", whitelist) is False
    assert check_whitelist("/etc/passwd", whitelist) is False


def test_check_whitelist_no_partial_match():
    # 路径分隔符检查：/home/user/docsx 不应匹配 /home/user/docs
    whitelist = ["/home/user/docs"]
    assert check_whitelist("/home/user/docsx/file.txt", whitelist) is False
    assert check_whitelist("/home/user/docs_backup", whitelist) is False


def test_check_whitelist_exact_match():
    whitelist = ["/home/user/docs"]
    assert check_whitelist("/home/user/docs", whitelist) is True


def test_check_whitelist_relative_path():
    whitelist = ["/tmp/test"]
    os.makedirs("/tmp/test/sub", exist_ok=True)
    result = check_whitelist("/tmp/test/file.txt", whitelist)
    assert result is True


def test_check_whitelist_rejects_symlink_escape(tmp_path):
    allowed = tmp_path / "allowed"
    outside = tmp_path / "outside"
    allowed.mkdir()
    outside.mkdir()
    secret = outside / "secret.txt"
    secret.write_text("secret")

    link = allowed / "link"
    try:
        link.symlink_to(secret)
    except OSError:
        return

    assert check_whitelist(str(link), [str(allowed)]) is False
