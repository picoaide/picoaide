"""登录窗口"""

from PySide6.QtWidgets import (
  QDialog, QVBoxLayout, QHBoxLayout, QLabel,
  QLineEdit, QPushButton, QCheckBox, QSizePolicy,
)
from PySide6.QtCore import Qt


class LoginWindow(QDialog):
  def __init__(self, cfg, parent=None):
    super().__init__(parent)
    self.cfg = cfg
    self.result_data = None
    self._setup_ui()

  def _setup_ui(self):
    self.setWindowTitle("PicoAide Desktop - 登录")
    self.setMinimumWidth(480)
    self.setWindowFlags(self.windowFlags() & ~Qt.WindowContextHelpButtonHint)

    layout = QVBoxLayout(self)
    layout.setSpacing(10)
    layout.setContentsMargins(36, 28, 36, 24)

    # 标题
    title = QLabel("PicoAide Desktop")
    title.setProperty("class", "title")
    title.setAlignment(Qt.AlignCenter)
    layout.addWidget(title)

    subtitle = QLabel("连接到 PicoAide 服务器")
    subtitle.setProperty("class", "subtitle")
    subtitle.setAlignment(Qt.AlignCenter)
    layout.addWidget(subtitle)

    layout.addSpacing(12)

    # 服务器地址
    layout.addWidget(QLabel("服务器地址"))
    self.server_input = QLineEdit()
    self.server_input.setPlaceholderText("例如: http://10.88.7.22")
    self.server_input.setText(self.cfg.get("server_url", ""))
    self.server_input.setMinimumHeight(36)
    self.server_input.setSizePolicy(QSizePolicy.Expanding, QSizePolicy.Fixed)
    layout.addWidget(self.server_input)

    # 用户名 + 密码 横向排列
    form_row = QHBoxLayout()
    form_row.setSpacing(12)

    left = QVBoxLayout()
    left.setSpacing(4)
    left.addWidget(QLabel("用户名"))
    self.user_input = QLineEdit()
    self.user_input.setPlaceholderText("用户名")
    self.user_input.setText(self.cfg.get("username", ""))
    self.user_input.setMinimumHeight(36)
    left.addWidget(self.user_input)
    form_row.addLayout(left)

    right = QVBoxLayout()
    right.setSpacing(4)
    right.addWidget(QLabel("密码"))
    self.pass_input = QLineEdit()
    self.pass_input.setPlaceholderText("密码")
    self.pass_input.setEchoMode(QLineEdit.Password)
    self.pass_input.setMinimumHeight(36)
    right.addWidget(self.pass_input)
    form_row.addLayout(right)

    layout.addLayout(form_row)

    # 自动连接
    self.auto_connect = QCheckBox("下次自动连接")
    self.auto_connect.setChecked(self.cfg.get("auto_connect", False))
    layout.addWidget(self.auto_connect)

    layout.addSpacing(12)

    # 登录按钮
    self.login_btn = QPushButton("登 录")
    self.login_btn.setProperty("class", "primary")
    self.login_btn.setFixedHeight(42)
    self.login_btn.setSizePolicy(QSizePolicy.Expanding, QSizePolicy.Fixed)
    self.login_btn.clicked.connect(self._on_login)
    layout.addWidget(self.login_btn)

    # 消息
    self.msg_label = QLabel("")
    self.msg_label.setAlignment(Qt.AlignCenter)
    self.msg_label.setWordWrap(True)
    self.msg_label.setProperty("class", "subtitle")
    layout.addWidget(self.msg_label)

  def _on_login(self):
    server = self.server_input.text().strip().rstrip("/")
    username = self.user_input.text().strip()
    password = self.pass_input.text()

    if not server:
      self._show_msg("请输入服务器地址")
      return
    if not username or not password:
      self._show_msg("请输入用户名和密码")
      return

    self.login_btn.setEnabled(False)
    self._show_msg("正在连接...", False)

    from core.connection import Connection
    conn = Connection()
    ok, result = conn.login(server, username, password)

    if ok:
      self.result_data = {
        "server_url": server,
        "username": username,
        "mcp_token": result,
        "auto_connect": self.auto_connect.isChecked(),
      }
      self.accept()
    else:
      self._show_msg(result)
      self.login_btn.setEnabled(True)

  def _show_msg(self, text, is_error=True):
    self.msg_label.setText(text)
    self.msg_label.setProperty("class", "status-err" if is_error else "subtitle")
    self.msg_label.style().unpolish(self.msg_label)
    self.msg_label.style().polish(self.msg_label)
