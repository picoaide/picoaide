"""本地配置管理"""

import json
import os
import sys

from .permissions import get_default_permissions

def _config_dir():
  """获取配置文件目录"""
  if sys.platform == "win32":
    base = os.environ.get("APPDATA", os.path.expanduser("~"))
  elif sys.platform == "darwin":
    base = os.path.expanduser("~/Library/Application Support")
  else:
    base = os.environ.get("XDG_CONFIG_HOME", os.path.expanduser("~/.config"))
  d = os.path.join(base, "picoaide-desktop")
  os.makedirs(d, exist_ok=True)
  return d

def _config_path():
  return os.path.join(_config_dir(), "config.json")

def load_config():
  """加载配置，不存在则返回默认值"""
  defaults = {
    "server_url": "",
    "username": "",
    "session_cookie": "",
    "mcp_token": "",
    "permissions": get_default_permissions(),
    "whitelist_dirs": [],
    "auto_connect": False,
    "minimize_to_tray": True,
  }
  try:
    with open(_config_path(), "r", encoding="utf-8") as f:
      saved = json.load(f)
    defaults.update(saved)
  except (FileNotFoundError, json.JSONDecodeError):
    pass
  return defaults

def save_config(cfg):
  """保存配置到文件"""
  with open(_config_path(), "w", encoding="utf-8") as f:
    json.dump(cfg, f, ensure_ascii=False, indent=2)
