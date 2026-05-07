import sys
import types
from unittest.mock import MagicMock

# Mock heavy dependencies before any project imports
heavy_modules = [
    "pyautogui",
    "PIL",
    "PIL.Image",
    "PIL.ImageGrab",
    "mss",
    "rapidocr_onnxruntime",
    "numpy",
    "openpyxl",
    "docx",
]

for mod_name in heavy_modules:
    if mod_name not in sys.modules:
        sys.modules[mod_name] = MagicMock()

# pyautogui 需要一些属性
if "pyautogui" in sys.modules:
    sys.modules["pyautogui"].FAILSAFE = False
    sys.modules["pyautogui"].PAUSE = 0
