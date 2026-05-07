import sys
import os

# 确保 core 包可导入
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from core.permissions import (
    get_default_permissions,
    is_tool_allowed,
    get_allowed_tools,
    PERMISSION_GROUPS,
    TOOL_GROUP_MAP,
)


def test_get_default_permissions():
    perms = get_default_permissions()
    assert isinstance(perms, dict)
    assert "screenshot" in perms
    assert "mouse" in perms
    assert "keyboard" in perms
    assert "file_read" in perms
    assert "file_write" in perms
    assert "file_list" in perms

    # 默认值检查
    assert perms["screenshot"] is True
    assert perms["mouse"] is True
    assert perms["keyboard"] is True
    assert perms["file_read"] is True
    assert perms["file_write"] is False
    assert perms["file_list"] is True


def test_is_tool_allowed():
    perms = get_default_permissions()

    # 已授权的工具
    assert is_tool_allowed("computer_screenshot", perms) is True
    assert is_tool_allowed("computer_mouse_click", perms) is True
    assert is_tool_allowed("computer_keyboard_type", perms) is True

    # file_write 默认禁用
    assert is_tool_allowed("computer_file_write", perms) is False

    # 未知工具
    assert is_tool_allowed("unknown_tool", perms) is False


def test_is_tool_allowed_custom():
    perms = {
        "screenshot": False,
        "mouse": True,
    }

    assert is_tool_allowed("computer_screenshot", perms) is False
    assert is_tool_allowed("computer_mouse_click", perms) is True
    assert is_tool_allowed("computer_keyboard_type", perms) is False


def test_get_allowed_tools():
    perms = get_default_permissions()
    tools = get_allowed_tools(perms)

    assert "computer_screenshot" in tools
    assert "computer_mouse_click" in tools
    assert "computer_keyboard_type" in tools
    assert "computer_file_read" in tools

    # file_write 默认禁用
    assert "computer_file_write" not in tools


def test_get_allowed_tools_none_enabled():
    perms = {key: False for key in PERMISSION_GROUPS}
    tools = get_allowed_tools(perms)
    assert tools == []


def test_get_allowed_tools_all_enabled():
    perms = {key: True for key in PERMISSION_GROUPS}
    tools = get_allowed_tools(perms)

    # 所有工具数量应等于所有分组工具总数
    total_tools = sum(len(g["tools"]) for g in PERMISSION_GROUPS.values())
    assert len(tools) == total_tools


def test_tool_group_map_completeness():
    # 每个 PERMISSION_GROUPS 中的工具都应在 TOOL_GROUP_MAP 中
    for group_key, group_info in PERMISSION_GROUPS.items():
        for tool in group_info["tools"]:
            assert tool in TOOL_GROUP_MAP
            assert TOOL_GROUP_MAP[tool] == group_key
