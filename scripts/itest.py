#!/usr/bin/env python3
"""PicoAide 集成测试工具.

所有 API 均使用 application/x-www-form-urlencoded 格式。
自动处理 Cookie 和 CSRF。

用法:
  python3 scripts/itest.py <服务器IP> [--fix-admin]
    --fix-admin  重置 admin 密码为 admin123（通过 SSH reset + API change）
"""

import argparse
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
import urllib.parse
from http.cookiejar import CookieJar
from typing import Optional


class PicoAideClient:
  """自动管理 Cookie 和 CSRF 的 API 客户端."""

  def __init__(self, host: str):
    self.base = f"http://{host}:80"
    self.cookiejar = CookieJar()
    self.opener = urllib.request.build_opener(
      urllib.request.HTTPCookieProcessor(self.cookiejar))
    self.csrf_token: Optional[str] = None

  def _form(self, method: str, path: str, data: dict = None) -> dict:
    """发送 form-urlencoded 请求（自动注入 csrf_token）. """
    url = f"{self.base}{path}"
    if data is None:
      data = {}
    if self.csrf_token and method == "POST" and "csrf_token" not in data:
      data["csrf_token"] = self.csrf_token
    body = urllib.parse.urlencode(data).encode() if data else None
    req = urllib.request.Request(url, data=body, method=method)
    if body:
      req.add_header("Content-Type", "application/x-www-form-urlencoded")
    try:
      resp = self.opener.open(req)
      return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
      body = e.read().decode()
      try:
        return json.loads(body)
      except json.JSONDecodeError:
        return {"success": False, "error": body[:300]}

  def GET(self, path: str) -> dict:
    return self._form("GET", path)

  def POST(self, path: str, data: dict = None) -> dict:
    return self._form("POST", path, data)

  def login(self, username: str, password: str) -> bool:
    resp = self.POST("/api/login", {"username": username, "password": password})
    if resp.get("success"):
      csrf_resp = self.GET("/api/csrf")
      self.csrf_token = csrf_resp.get("csrf_token")
      return True
    print(f"  登录失败: {resp.get('error', '')}")
    return False

  def SSE(self, path: str, data: dict, timeout: int = 45) -> list:
    """发送 POST 请求并读取 SSE 响应流."""
    url = f"{self.base}{path}"
    body = urllib.parse.urlencode(data).encode()
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")
    req.add_header("Accept", "text/event-stream")
    if self.csrf_token:
      req.add_header("X-CSRF-Token", self.csrf_token)

    events = []
    try:
      resp = self.opener.open(req, timeout=timeout)
      buf = ""
      while True:
        chunk = resp.read(1024)
        if not chunk:
          break
        buf += chunk.decode()
        while "\n\n" in buf:
          raw, buf = buf.split("\n\n", 1)
          for line in raw.split("\n"):
            dl = line.strip()
            if dl.startswith("data: "):
              payload = dl[6:].strip()
              if payload == "[DONE]":
                events.append({"type": "done"})
              elif payload.startswith("[ERROR]"):
                events.append({"type": "error", "data": payload[7:].strip()})
              elif payload.startswith("[USAGE]"):
                try:
                  events.append({"type": "usage", "data": json.loads(payload[7:])})
                except:
                  events.append({"type": "usage", "raw": payload[7:]})
              else:
                try:
                  text = json.loads(payload)
                  events.append({"type": "text_delta", "data": {"text": text}})
                except json.JSONDecodeError:
                  events.append({"type": "data", "raw": payload[:200]})
    except Exception as e:
      print(f"  SSE 读取完毕: {e}")
    return events


def section(title: str):
  print(f"\n{'=' * 60}")
  print(f"  {title}")
  print(f"{'=' * 60}")


# ---- 测试用例 ----

def test_health(c: PicoAideClient):
  section("健康检查")
  r = c.GET("/api/health")
  assert r.get("status") == "ok", f"health failed: {r}"
  print(f"  ✓ 服务正常 version={r.get('version','?')}")


def test_admin_login(c: PicoAideClient, password: str):
  section("管理员登录")
  assert c.login("admin", password), "登录失败"
  print(f"  ✓ 登录成功")


def test_user_info(c: PicoAideClient):
  section("用户信息")
  r = c.GET("/api/user/info")
  assert r.get("success"), f"获取用户信息失败: {r}"
  print(f"  ✓ {r.get('username')} 角色={r.get('role')}")


def test_create_user(c: PicoAideClient, username: str):
  section(f"创建用户: {username}")
  r = c.POST("/api/admin/users/create", {"username": username})
  if r.get("success"):
    pwd = r.get('password', '')
    print(f"  ✓ 创建成功 密码={'****' if pwd else '无'}")
    return r.get("password")
  print(f"  注意: {r.get('error', '')}")
  return None


def test_delete_user(c: PicoAideClient, username: str):
  section(f"删除用户: {username}")
  r = c.POST("/api/admin/users/delete", {"username": username})
  if r.get("success"):
    print(f"  ✓ 已删除")
  else:
    print(f"  注意: {r.get('error', '')}")


def test_user_login(c: PicoAideClient, username: str, password: str):
  section(f"普通用户登录: {username}")
  c.cookiejar.clear()
  c.csrf_token = None
  assert c.login(username, password), f"用户 {username} 登录失败"
  print(f"  ✓ 登录成功")


def test_chat_send(c: PicoAideClient):
  section("Chat SSE")
  events = c.SSE("/api/user/chat/send", {"message": "请用一句话介绍你自己"}, timeout=25)
  print(f"  收到 {len(events)} 个事件")
  for ev in events:
    t = ev.get("type", "?")
    if t == "text_delta":
      print(f"  text: {ev.get('data', {}).get('text', '')[:80]}")
    elif t == "finish":
      usage = ev.get("data", {}).get("usage", {})
      print(f"  finish usage={usage}")
    elif t == "error":
      print(f"  ERROR: {ev}")
    elif t == "done":
      print(f"  [DONE]")
    else:
      print(f"  {t}: {json.dumps(ev, ensure_ascii=False)[:100]}")
  has_text = any(e.get("type") == "text_delta" for e in events)
  has_finish = any(e.get("type") == "finish" for e in events)
  if has_text:
    print(f"  ✓ 收到文本")
  if has_finish:
    print(f"  ✓ 收到 finish")
  if not events:
    print(f"  ✗ 未收到任何事件")


def test_chat_history(c: PicoAideClient):
  section("Chat 历史")
  r = c.GET("/api/user/chat/history")
  if r.get("success"):
    msgs = r.get("messages") or []
    print(f"  ✓ {len(msgs)} 条消息")
    for m in msgs:
      print(f"    [{m.get('role','?')}] {m.get('content','')[:60]}")
  else:
    print(f"  注意: {r.get('error', '')}")


def test_admin_users(c: PicoAideClient):
  section("管理员: 用户列表")
  r = c.GET("/api/admin/users")
  assert r.get("success"), f"获取用户列表失败: {r}"
  users = r.get("users", [])
  print(f"  ✓ {len(users)} 人")
  for u in users:
    print(f"    - {u.get('username')} [{u.get('role')}] source={u.get('source')}")


def test_admin_skills(c: PicoAideClient):
  section("管理员: 技能列表")
  r = c.GET("/api/admin/skills")
  if r.get("success"):
    skills = r.get("skills", [])
    print(f"  ✓ {len(skills)} 个技能")
    for s in skills:
      print(f"    - {s.get('name')} v{s.get('version','?')}")
  else:
    print(f"  (无技能或查询失败)")


def fix_admin_password(host: str) -> str:
  """重置 admin 密码为 admin123"""
  # 第 1 步：通过 SSH 生成随机密码
  result = subprocess.run(
    ["ssh", f"root@{host}", "/usr/sbin/picoaide reset-password admin"],
    capture_output=True, text=True, timeout=10)
  output = result.stdout + result.stderr
  m = re.search(r"密码已重置: (\S+)", output)
  if not m:
    raise RuntimeError(f"SSH reset-password 失败: {result.stdout} {result.stderr}")
  tmp_pwd = m.group(1)
  print(f"  临时密码已获取")

  # 第 2 步：登录并用 API 改密码
  c = PicoAideClient(host)
  assert c.login("admin", tmp_pwd), "以临时密码登录失败"
  # 通过 change-password 改为 admin123
  r = c.POST("/api/admin/password", {
    "old_password": tmp_pwd,
    "new_password": "admin123",
  })
  assert r.get("success"), f"修改密码失败: {r}"
  print(f"  ✓ admin 密码已设为 admin123")
  return "admin123"


def main():
  parser = argparse.ArgumentParser(description="PicoAide 集成测试")
  parser.add_argument("host", help="服务器 IP")
  parser.add_argument("--fix-admin", action="store_true", help="重置 admin 密码为 admin123")
  parser.add_argument("--password", default=None, help="admin 密码（默认 admin123）")
  parser.add_argument("--create-user", default="itest-py", help="测试用户名")
  args = parser.parse_args()

  password = args.password or "admin123"

  if args.fix_admin:
    print(">>> 修复 admin 密码...")
    password = fix_admin_password(args.host)

  c = PicoAideClient(args.host)

  tests = [
    ("健康检查", lambda: test_health(c)),
    ("管理员登录", lambda: test_admin_login(c, password)),
    ("用户信息", lambda: test_user_info(c)),
    ("管理员用户列表", lambda: test_admin_users(c)),
    ("技能列表", lambda: test_admin_skills(c)),
  ]

  for name, fn in tests:
    try:
      fn()
    except Exception as e:
      print(f"  ✗ {name} 失败: {e}")
      sys.exit(1)

  # 创建测试用户
  section("创建/清理测试用户")
  c.POST("/api/admin/users/delete", {"username": args.create_user})
  user_pwd = test_create_user(c, args.create_user)
  if not user_pwd:
    print("无法创建用户，跳过普通用户测试")
    sys.exit(1)

  # 普通用户测试
  section(f"普通用户测试 ({args.create_user})")
  try:
    test_user_login(c, args.create_user, user_pwd)
    test_chat_send(c)

    # 等助手回复写入历史（LLM 可能较慢）
    for i in range(30):
      r = c.GET("/api/user/chat/history")
      msgs = r.get("messages") or []
      if len(msgs) >= 2 and msgs[-1].get("role") == "assistant":
        print(f"  ✓ 收到助手回复（第 {i+1} 次轮询）")
        for m in msgs:
          print(f"    [{m.get('role','?')}] {m.get('content','')[:80]}")
        break
      time.sleep(2)
    else:
      print(f"  ⚠️ 等待超时，未收到助手回复")

  except Exception as e:
    print(f"  ✗ 普通用户测试失败: {e}")
    import traceback
    traceback.print_exc()

  # 切回管理员，清理
  print(f"\n--- 清理 ---")
  c.cookiejar.clear()
  c.csrf_token = None
  assert c.login("admin", password), "重新登录失败"

  # 查询并设置正确密码
  test_create_user(c, args.create_user)  # re-create won't work, but delete first
  test_delete_user(c, args.create_user)

  print(f"\n{'=' * 60}")
  print(f"  测试结束")
  print(f"{'=' * 60}")


if __name__ == "__main__":
  main()
