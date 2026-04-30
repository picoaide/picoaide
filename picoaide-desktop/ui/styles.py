"""PySide6 暗色主题样式"""

DARK_STYLE = """
QMainWindow, QDialog {
  background-color: #1a1a2e;
  color: #e0e0e0;
}

QWidget {
  background-color: transparent;
  color: #e0e0e0;
  font-family: "Microsoft YaHei", "PingFang SC", "Segoe UI", sans-serif;
  font-size: 13px;
}

QGroupBox {
  background-color: #16213e;
  border: 1px solid #0f3460;
  border-radius: 8px;
  margin-top: 12px;
  padding: 16px 12px 12px 12px;
}
QGroupBox::title {
  subcontrol-origin: margin;
  left: 14px;
  padding: 0 6px;
  color: #e94560;
  font-size: 14px;
  font-weight: bold;
}

QPushButton {
  background-color: #0f3460;
  border: none;
  border-radius: 6px;
  padding: 8px 18px;
  color: #e0e0e0;
  font-size: 13px;
}
QPushButton:hover {
  background-color: #1a4a8a;
}
QPushButton:pressed {
  background-color: #0a2540;
}
QPushButton:disabled {
  background-color: #2a2a3e;
  color: #666;
}
QPushButton[class="primary"] {
  background-color: #e94560;
  font-weight: bold;
}
QPushButton[class="primary"]:hover {
  background-color: #ff5a75;
}
QPushButton[class="danger"] {
  background-color: #c0392b;
}
QPushButton[class="danger"]:hover {
  background-color: #e74c3c;
}
QPushButton[class="outline"] {
  background-color: transparent;
  border: 1px solid #0f3460;
  color: #e0e0e0;
}
QPushButton[class="outline"]:hover {
  background-color: #0f3460;
}

QLineEdit, QTextEdit {
  background-color: #16213e;
  border: 1px solid #0f3460;
  border-radius: 6px;
  padding: 8px 12px;
  color: #e0e0e0;
  selection-background-color: #e94560;
}
QLineEdit:focus, QTextEdit:focus {
  border-color: #e94560;
}

QCheckBox {
  spacing: 8px;
  color: #e0e0e0;
  font-size: 13px;
}
QCheckBox::indicator {
  width: 18px;
  height: 18px;
  border-radius: 4px;
  border: 2px solid #555;
  background-color: #1a1a2e;
}
QCheckBox::indicator:checked {
  background-color: #e94560;
  border-color: #e94560;
  image: url(data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' fill='white'><path d='M12.207 4.793a1 1 0 010 1.414l-5 5a1 1 0 01-1.414 0l-2-2a1 1 0 011.414-1.414L6.5 9.086l4.293-4.293a1 1 0 011.414 0z'/></svg>);
}
QCheckBox::indicator:hover {
  border-color: #888;
}

QLabel[class="title"] {
  font-size: 18px;
  font-weight: bold;
  color: #ffffff;
}
QLabel[class="subtitle"] {
  font-size: 12px;
  color: #888;
}
QLabel[class="status-ok"] {
  color: #2ecc71;
  font-weight: bold;
}
QLabel[class="status-err"] {
  color: #e74c3c;
  font-weight: bold;
}
QLabel[class="desc"] {
  color: #999;
  font-size: 12px;
  padding-left: 28px;
}

QListWidget {
  background-color: #16213e;
  border: 1px solid #0f3460;
  border-radius: 6px;
  padding: 4px;
}
QListWidget::item {
  padding: 6px 8px;
  border-radius: 4px;
}
QListWidget::item:hover {
  background-color: #1a2a4e;
}

QScrollBar:vertical {
  background: #1a1a2e;
  width: 8px;
  border-radius: 4px;
}
QScrollBar::handle:vertical {
  background: #0f3460;
  border-radius: 4px;
  min-height: 30px;
}
QScrollBar::add-line:vertical, QScrollBar::sub-line:vertical {
  height: 0;
}

QToolButton {
  background-color: transparent;
  border: none;
  color: #e0e0e0;
  padding: 4px 8px;
  border-radius: 4px;
}
QToolButton:hover {
  background-color: #0f3460;
}

QComboBox {
  background-color: #16213e;
  border: 1px solid #0f3460;
  border-radius: 6px;
  padding: 6px 12px;
  color: #e0e0e0;
}
QComboBox::drop-down {
  border: none;
}
QComboBox QAbstractItemView {
  background-color: #16213e;
  border: 1px solid #0f3460;
  color: #e0e0e0;
  selection-background-color: #0f3460;
}
"""
