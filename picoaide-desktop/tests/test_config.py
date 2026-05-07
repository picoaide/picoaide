import sys
import os
import json
from unittest.mock import patch

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from core.config import load_config, save_config


def test_load_config_default(tmp_path):
    config_dir = str(tmp_path / "picoaide-desktop")
    config_file = os.path.join(config_dir, "config.json")

    with patch("core.config._config_path", return_value=config_file):
        cfg = load_config()

    assert cfg["server_url"] == ""
    assert cfg["username"] == ""
    assert cfg["mcp_token"] == ""
    assert cfg["whitelist_dirs"] == []
    assert cfg["auto_connect"] is False
    assert cfg["minimize_to_tray"] is True
    assert isinstance(cfg["permissions"], dict)
    assert "screenshot" in cfg["permissions"]


def test_load_config_with_file(tmp_path):
    config_dir = str(tmp_path / "picoaide-desktop")
    os.makedirs(config_dir, exist_ok=True)
    config_file = os.path.join(config_dir, "config.json")

    test_data = {
        "server_url": "http://10.0.0.1",
        "username": "testuser",
        "permissions": {"screenshot": False, "mouse": True},
    }
    with open(config_file, "w") as f:
        json.dump(test_data, f)

    with patch("core.config._config_path", return_value=config_file):
        cfg = load_config()

    assert cfg["server_url"] == "http://10.0.0.1"
    assert cfg["username"] == "testuser"
    assert cfg["permissions"]["screenshot"] is False
    assert cfg["permissions"]["mouse"] is True
    # 未指定的字段保留默认值
    assert cfg["auto_connect"] is False


def test_load_config_corrupt_file(tmp_path):
    config_dir = str(tmp_path / "picoaide-desktop")
    os.makedirs(config_dir, exist_ok=True)
    config_file = os.path.join(config_dir, "config.json")

    with open(config_file, "w") as f:
        f.write("not valid json{{{")

    with patch("core.config._config_path", return_value=config_file):
        cfg = load_config()

    # 损坏文件应返回默认值
    assert cfg["server_url"] == ""
    assert isinstance(cfg["permissions"], dict)


def test_save_and_load(tmp_path):
    config_dir = str(tmp_path / "picoaide-desktop")
    os.makedirs(config_dir, exist_ok=True)
    config_file = os.path.join(config_dir, "config.json")

    test_data = {
        "server_url": "http://example.com",
        "username": "user1",
        "mcp_token": "secret",
    }

    with patch("core.config._config_path", return_value=config_file):
        save_config(test_data)
        cfg = load_config()

    assert cfg["server_url"] == "http://example.com"
    assert cfg["username"] == "user1"
    assert cfg["mcp_token"] == "secret"
