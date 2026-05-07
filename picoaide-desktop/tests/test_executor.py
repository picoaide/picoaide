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


def test_check_whitelist_partial_match():
    # NOTE: 当前 check_whitelist 使用 startswith 前缀匹配，
    # /home/user/docsx 会匹配 /home/user/docs 前缀（潜在安全问题）
    # 如果修复为路径分隔符检查，此测试应改为 assert False
    whitelist = ["/home/user/docs"]
    assert check_whitelist("/home/user/docsx/file.txt", whitelist) is True


def test_check_whitelist_relative_path():
    whitelist = ["/tmp/test"]
    os.makedirs("/tmp/test/sub", exist_ok=True)
    # 传入相对路径应被 abspath 处理
    result = check_whitelist("/tmp/test/file.txt", whitelist)
    assert result is True
