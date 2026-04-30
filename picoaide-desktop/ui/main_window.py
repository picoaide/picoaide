"""主窗口：连接状态 + 权限管理 + 白名单目录"""

from PySide6.QtWidgets import (
  QMainWindow, QWidget, QVBoxLayout, QHBoxLayout, QGridLayout,
  QLabel, QPushButton, QCheckBox, QListWidget, QListWidgetItem,
  QFileDialog, QGroupBox, QSystemTrayIcon, QMenu, QComboBox,
  QStyle, QStyleOptionButton,
)
from PySide6.QtCore import Qt, QSize
from PySide6.QtGui import QAction, QIcon, QPainter, QPen, QColor

from core.permissions import PERMISSION_GROUPS
from core.config import save_config


class CheckCheckBox(QCheckBox):
  """自定义 Checkbox：checked 状态下绘制白色对号"""

  def paintEvent(self, event):
    super().paintEvent(event)
    if self.isChecked():
      opt = QStyleOptionButton()
      self.initStyleOption(opt)
      rect = self.style().subElementRect(QStyle.SE_CheckBoxIndicator, opt, self)
      painter = QPainter(self)
      painter.setRenderHint(QPainter.Antialiasing)
      pen = QPen(QColor("white"), 2)
      painter.setPen(pen)
      r = rect.adjusted(3, 3, -3, -3)
      painter.drawLine(r.left(), r.center().y(), r.center().x(), r.bottom())
      painter.drawLine(r.center().x(), r.bottom(), r.right(), r.top())
      painter.end()


class MainWindow(QMainWindow):
  def __init__(self, cfg, connection):
    super().__init__()
    self.cfg = cfg
    self.conn = connection
    self.perm_checks = {}
    self._setup_ui()
    self._setup_tray()
    self.conn.on_status_change = self._update_status

  def _setup_ui(self):
    self.setWindowTitle("PicoAide Desktop")
    self.setMinimumSize(520, 690)
    self.resize(560, 750)

    central = QWidget()
    self.setCentralWidget(central)
    layout = QVBoxLayout(central)
    layout.setSpacing(14)
    layout.setContentsMargins(24, 20, 24, 20)

    # 顶栏：状态 + 服务器信息
    top = QHBoxLayout()
    self.status_dot = QLabel("●")
    self.status_dot.setFixedWidth(20)
    self.status_label = QLabel("未连接")
    top.addWidget(self.status_dot)
    top.addWidget(self.status_label)
    top.addStretch()

    server_info = QLabel(self.cfg.get("server_url", ""))
    server_info.setProperty("class", "subtitle")
    top.addWidget(server_info)

    self.disconnect_btn = QPushButton("断开")
    self.disconnect_btn.setProperty("class", "outline")
    self.disconnect_btn.setFixedWidth(70)
    self.disconnect_btn.clicked.connect(self._on_disconnect)
    top.addWidget(self.disconnect_btn)
    layout.addLayout(top)

    # 授权管理
    perm_group = QGroupBox("授权管理")
    perm_layout = QVBoxLayout(perm_group)
    perm_layout.setSpacing(6)

    # 全选/全不选
    perm_header = QHBoxLayout()
    self.select_all_btn = QPushButton("全部授权")
    self.select_all_btn.setProperty("class", "primary")
    self.select_all_btn.setMinimumWidth(100)
    self.select_all_btn.clicked.connect(lambda: self._set_all(True))
    perm_header.addWidget(self.select_all_btn)

    self.deselect_all_btn = QPushButton("全部禁用")
    self.deselect_all_btn.setProperty("class", "outline")
    self.deselect_all_btn.setMinimumWidth(100)
    self.deselect_all_btn.clicked.connect(lambda: self._set_all(False))
    perm_header.addWidget(self.deselect_all_btn)
    perm_header.addStretch()
    perm_layout.addLayout(perm_header)

    # 各权限项
    permissions = self.cfg.get("permissions", {})
    for key, info in PERMISSION_GROUPS.items():
      cb = CheckCheckBox(f"  {info['icon']}  {info['label']}")
      cb.setChecked(permissions.get(key, info["default"]))
      cb.stateChanged.connect(self._on_perm_changed)
      perm_layout.addWidget(cb)
      self.perm_checks[key] = cb

      desc = QLabel(info["desc"])
      desc.setProperty("class", "desc")
      perm_layout.addWidget(desc)

    layout.addWidget(perm_group, stretch=0)

    # 白名单目录
    wl_group = QGroupBox("白名单目录")
    wl_layout = QVBoxLayout(wl_group)

    wl_hint = QLabel("文件操作仅允许在以下目录内执行")
    wl_hint.setProperty("class", "desc")
    wl_hint.setStyleSheet("padding-left: 0")
    wl_layout.addWidget(wl_hint)

    wl_btn_row = QHBoxLayout()
    add_dir_btn = QPushButton("添加目录")
    add_dir_btn.setProperty("class", "outline")
    add_dir_btn.clicked.connect(self._add_whitelist_dir)
    wl_btn_row.addWidget(add_dir_btn)

    add_home_btn = QPushButton("添加主目录")
    add_home_btn.setProperty("class", "outline")
    add_home_btn.clicked.connect(self._add_home_dir)
    wl_btn_row.addWidget(add_home_btn)

    add_docs_btn = QPushButton("添加文档目录")
    add_docs_btn.setProperty("class", "outline")
    add_docs_btn.clicked.connect(self._add_docs_dir)
    wl_btn_row.addWidget(add_docs_btn)
    wl_btn_row.addStretch()
    wl_layout.addLayout(wl_btn_row)

    self.wl_list = QListWidget()
    self.wl_list.setMinimumHeight(60)
    for d in self.cfg.get("whitelist_dirs", []):
      self._add_wl_item(d)
    wl_layout.addWidget(self.wl_list)

    layout.addWidget(wl_group, stretch=1)

    # 底栏
    bottom = QHBoxLayout()
    save_btn = QPushButton("保存设置")
    save_btn.setProperty("class", "primary")
    save_btn.clicked.connect(self._save)
    bottom.addWidget(save_btn)
    bottom.addStretch()
    layout.addLayout(bottom)

  def _setup_tray(self):
    self.tray = QSystemTrayIcon(self)
    # 创建一个简单的托盘图标
    from PySide6.QtGui import QPixmap
    px = QPixmap(32, 32)
    px.fill(QColor("#e94560"))
    self.tray.setIcon(QIcon(px))
    self.tray.setToolTip("PicoAide Desktop")

    menu = QMenu()
    show_action = QAction("显示主窗口", self)
    show_action.triggered.connect(self._show_window)
    menu.addAction(show_action)

    quit_action = QAction("退出", self)
    quit_action.triggered.connect(self._quit)
    menu.addAction(quit_action)

    self.tray.setContextMenu(menu)
    self.tray.activated.connect(self._tray_activated)

  def show_tray(self):
    self.tray.show()

  def _tray_activated(self, reason):
    if reason == QSystemTrayIcon.DoubleClick:
      self._show_window()

  def _show_window(self):
    self.showNormal()
    self.activateWindow()

  def _quit(self):
    self.conn.disconnect()
    self._save()
    from PySide6.QtWidgets import QApplication
    QApplication.quit()

  def closeEvent(self, event):
    # 关闭窗口时最小化到托盘
    event.ignore()
    self.hide()

  def _update_status(self):
    if self.conn.connected:
      self.status_dot.setStyleSheet("color: #2ecc71; font-size: 18px;")
      self.status_dot.setText("●")
      self.status_label.setText("已连接")
      self.status_label.setProperty("class", "status-ok")
      self.disconnect_btn.setText("断开")
    else:
      self.status_dot.setStyleSheet("color: #e74c3c; font-size: 18px;")
      self.status_dot.setText("●")
      self.status_label.setText("未连接")
      self.status_label.setProperty("class", "status-err")
      self.disconnect_btn.setText("重连")
    self.status_label.style().unpolish(self.status_label)
    self.status_label.style().polish(self.status_label)

  def _on_disconnect(self):
    if self.conn.connected:
      self.conn.disconnect()
    else:
      # 重连
      from ui.login_window import LoginWindow
      self._do_login()

  def _do_login(self):
    from ui.login_window import LoginWindow
    dlg = LoginWindow(self.cfg, self)
    if dlg.exec() == LoginWindow.Accepted and dlg.result_data:
      data = dlg.result_data
      self.cfg.update(data)
      self.conn.connect(data["server_url"], data["mcp_token"])
      self._save()
      self.status_label.setText(self.cfg.get("server_url", ""))

  def _set_all(self, enabled):
    for cb in self.perm_checks.values():
      cb.setChecked(enabled)

  def _on_perm_changed(self):
    perms = {k: cb.isChecked() for k, cb in self.perm_checks.items()}
    self.cfg["permissions"] = perms
    self.conn.permissions = perms

  def _add_whitelist_dir(self):
    d = QFileDialog.getExistingDirectory(self, "选择白名单目录")
    if d:
      self._add_wl_item(d)
      self._sync_whitelist()

  def _add_home_dir(self):
    import os
    self._add_wl_item(os.path.expanduser("~"))
    self._sync_whitelist()

  def _add_docs_dir(self):
    import os
    docs = os.path.join(os.path.expanduser("~"), "Documents")
    if os.path.isdir(docs):
      self._add_wl_item(docs)
      self._sync_whitelist()

  def _add_wl_item(self, path):
    # 去重
    for i in range(self.wl_list.count()):
      if self.wl_list.item(i).data(Qt.UserRole) == path:
        return
    item = QListWidgetItem(path)
    item.setData(Qt.UserRole, path)
    item.setFlags(item.flags() | Qt.ItemIsUserCheckable)
    item.setCheckState(Qt.Checked)
    self.wl_list.addItem(item)

    # 双击删除
    self.wl_list.itemDoubleClicked.connect(self._remove_wl_item)

  def _remove_wl_item(self, item):
    self.wl_list.takeItem(self.wl_list.row(item))
    self._sync_whitelist()

  def _sync_whitelist(self):
    dirs = []
    for i in range(self.wl_list.count()):
      item = self.wl_list.item(i)
      if item.checkState() == Qt.Checked:
        dirs.append(item.data(Qt.UserRole))
    self.cfg["whitelist_dirs"] = dirs
    self.conn.whitelist_dirs = dirs

  def _save(self):
    self._on_perm_changed()
    self._sync_whitelist()
    save_config(self.cfg)
    self.status_label.setText("已保存")
    from PySide6.QtCore import QTimer
    QTimer.singleShot(2000, lambda: self._update_status())
