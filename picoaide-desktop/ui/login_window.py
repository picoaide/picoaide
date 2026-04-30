"""登录窗口"""

from PySide6.QtWidgets import (
  QDialog, QVBoxLayout, QHBoxLayout, QLabel,
  QLineEdit, QPushButton, QMessageBox, QCheckBox,
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
    self.setFixedSize(420, 320)
    self.setWindowFlags(self.windowFlags() & ~Qt.WindowContextHelpButtonHint)

    layout = QVBoxLayout(self)
    layout.setSpacing(12)
    layout.setContentsMargins(32, 28, 32, 24)

    # 标题
    title = QLabel("PicoAide Desktop")
    title.setProperty("class", "title")
    title.setAlignment(Qt.AlignCenter)
    layout.addWidget(title)

    subtitle = QLabel("连接到 PicoAide 服务器")
    subtitle.setProperty("class", "subtitle")
    subtitle.setAlignment(Qt.AlignCenter)
    layout.addWidget(subtitle)

    layout.addSpacing(8)

    # 服务器地址
    self.server_input = QLineEdit()
    self.server_input.setPlaceholderText("服务器地址 (如 http://10.88.7.22:80)")
    self.server_input.setText(self.cfg.get("server_url", ""))
    layout.addWidget(QLabel("服务器地址"))
    layout.addWidget(self.server_input)

    # 用户名
    self.user_input = QLineEdit()
    self.user_input.setPlaceholderText("用户名")
    self.user_input.setText(self.cfg.get("username", ""))
    layout.addWidget(QLabel("用户名"))
    layout.addWidget(self.user_input)

    # 密码
    self.pass_input = QLineEdit()
    self.pass_input.setPlaceholderText("密码")
    self.pass_input.setEchoMode(QLineEdit.Password)
    layout.addWidget(QLabel("密码"))
    layout.addWidget(self.pass_input)

    # 自动连接
    self.auto_connect = QCheckBox("下次自动连接")
    self.auto_connect.setChecked(self.cfg.get("auto_connect", False))
    layout.addWidget(self.auto_connect)

    layout.addSpacing(8)

    # 登录按钮
    btn_layout = QHBoxLayout()
    self.login_btn = QPushButton("登 录")
    self.login_btn.setProperty("class", "primary")
    self.login_btn.setFixedHeight(40)
    self.login_btn.clicked.connect(self._on_login)
    btn_layout.addWidget(self.login_btn)
    layout.addLayout(btn_layout)

    # 消息
    self.msg_label = QLabel("")
    self.msg_label.setAlignment(Qt.AlignCenter)
    self.msg_label.setProperty("class", "subtitle")
    layout.addWidget(self.msg_label)

  def _on_login(self):
    server = self.server_input.text().strip().rstrip("/")
    username = self.user_input.text().strip()
    password = self.pass_input.text()

    if not server:
      self.msg_label.setText("请输入服务器地址")
      self.msg_label.setProperty("class", "status-err")
      return
    if not username or not password:
      self.msg_label.setText("请输入用户名和密码")
      self.msg_label.setProperty("class", "status-err")
      return

    self.login_btn.setEnabled(False)
    self.msg_label.setText("正在连接...")
    self.msg_label.setProperty("class", "subtitle")

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
      self.msg_label.setText(result)
      self.msg_label.setProperty("class", "status-err")
      self.login_btn.setEnabled(True)
