"""PicoAide Desktop Agent 入口"""

import sys
import logging
import platform

# Windows DPI 感知设置（必须在 QApplication 之前）
if platform.system() == "Windows":
  import ctypes
  try:
    ctypes.windll.shcore.SetProcessDpiAwareness(2)  # PROCESS_PER_MONITOR_DPI_AWARE_V2
  except Exception:
    try:
      ctypes.windll.user32.SetProcessDPIAware()
    except Exception:
      pass

from PySide6.QtWidgets import QApplication

from core.config import load_config, save_config
from core.connection import Connection
from core.permissions import get_default_permissions
from ui.styles import DARK_STYLE
from ui.login_window import LoginWindow
from ui.main_window import MainWindow
from version import VERSION

logging.basicConfig(
  level=logging.INFO,
  format="%(asctime)s [%(name)s] %(levelname)s: %(message)s",
  datefmt="%H:%M:%S",
)


def main():
  app = QApplication(sys.argv)
  app.setQuitOnLastWindowClosed(False)
  app.setStyleSheet(DARK_STYLE)
  app.setApplicationName("PicoAide Desktop")
  app.setApplicationVersion(VERSION)

  cfg = load_config()
  # 确保权限字段完整
  defaults = get_default_permissions()
  for k, v in defaults.items():
    cfg.setdefault("permissions", {}).setdefault(k, v)

  conn = Connection()

  # 自动连接逻辑
  if cfg.get("auto_connect") and cfg.get("mcp_token") and cfg.get("server_url"):
    conn.permissions = cfg["permissions"]
    conn.whitelist_dirs = cfg.get("whitelist_dirs", [])
    conn.connect(cfg["server_url"], cfg["mcp_token"])
    window = MainWindow(cfg, conn)
    window.show_tray()
  else:
    # 显示登录窗口
    login = LoginWindow(cfg)
    if login.exec() != LoginWindow.Accepted or not login.result_data:
      sys.exit(0)

    cfg.update(login.result_data)
    conn.permissions = cfg.get("permissions", get_default_permissions())
    conn.whitelist_dirs = cfg.get("whitelist_dirs", [])
    save_config(cfg)

    conn.connect(cfg["server_url"], cfg["mcp_token"])
    window = MainWindow(cfg, conn)
    window.show()
    window.show_tray()

  sys.exit(app.exec())


if __name__ == "__main__":
  main()
