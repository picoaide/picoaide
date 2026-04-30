"""WebSocket 连接管理"""

import json
import logging
import threading
import urllib.request
import urllib.parse

import websockets

from .executor import execute_tool

logger = logging.getLogger(__name__)


class Connection:
  """管理到 PicoAide 服务器的 WebSocket 连接"""

  def __init__(self, on_status_change=None):
    self.ws = None
    self.connected = False
    self._stop_event = threading.Event()
    self._thread = None
    self.permissions = {}
    self.whitelist_dirs = []
    self.on_status_change = on_status_change or (lambda: None)

  def login(self, server_url, username, password):
    """登录并获取 MCP token，返回 (success, message)"""
    base = server_url.rstrip("/")
    try:
      # 登录
      data = urllib.parse.urlencode({
        "username": username,
        "password": password,
      }).encode()
      req = urllib.request.Request(base + "/api/login", data=data)
      req.add_header("Content-Type", "application/x-www-form-urlencoded")
      with urllib.request.urlopen(req, timeout=10) as resp:
        result = json.loads(resp.read())
      if not result.get("success"):
        return False, result.get("error", "登录失败")

      # 提取 session cookie
      cookies = resp.headers.get_all("Set-Cookie")
      cookie_str = ""
      if cookies:
        parts = []
        for c in cookies:
          parts.append(c.split(";")[0])
        cookie_str = "; ".join(parts)

      # 获取 MCP token
      req2 = urllib.request.Request(base + "/api/mcp/token")
      req2.add_header("Cookie", cookie_str)
      with urllib.request.urlopen(req2, timeout=10) as resp2:
        token_result = json.loads(resp2.read())
      if not token_result.get("success"):
        return False, token_result.get("error", "获取 MCP token 失败")

      mcp_token = token_result["token"]
      return True, mcp_token

    except urllib.error.HTTPError as e:
      body = ""
      try:
        body = json.loads(e.read()).get("error", "")
      except Exception:
        pass
      return False, body or f"HTTP {e.code}"
    except Exception as e:
      return False, str(e)

  def connect(self, server_url, mcp_token):
    """启动 WebSocket 连接"""
    if self.connected:
      self.disconnect()

    self._stop_event.clear()
    base = server_url.rstrip("/")
    ws_url = base.replace("http://", "ws://").replace("https://", "wss://")
    ws_url += f"/api/computer/ws?token={urllib.parse.quote(mcp_token)}"

    self._thread = threading.Thread(target=self._run, args=(ws_url,), daemon=True)
    self._thread.start()

  def disconnect(self):
    """断开连接"""
    self._stop_event.set()
    if self.ws:
      try:
        import asyncio
        asyncio.get_event_loop().run_until_complete(self.ws.close())
      except Exception:
        pass
      self.ws = None
    self.connected = False
    self.on_status_change()

  def _run(self, ws_url):
    """WebSocket 连接主循环"""
    import asyncio

    async def _async_run():
      try:
        async with websockets.connect(ws_url, ping_interval=30) as ws:
          self.ws = ws
          self.connected = True
          self.on_status_change()
          logger.info("WebSocket 已连接")

          while not self._stop_event.is_set():
            try:
              msg = await asyncio.wait_for(ws.recv(), timeout=1)
            except asyncio.TimeoutError:
              continue
            except websockets.ConnectionClosed:
              break

            await self._handle_message(ws, msg)

      except Exception as e:
        logger.error("WebSocket 连接失败: %s", e)
      finally:
        self.connected = False
        self.ws = None
        self.on_status_change()

    try:
      asyncio.run(_async_run())
    except RuntimeError:
      loop = asyncio.new_event_loop()
      loop.run_until_complete(_async_run())
      loop.close()

  async def _handle_message(self, ws, raw_msg):
    """处理从服务器收到的工具调用命令"""
    try:
      msg = json.loads(raw_msg)
    except json.JSONDecodeError:
      return

    msg_id = msg.get("id")
    tool = msg.get("tool")
    params = msg.get("params", {})

    if not msg_id or not tool:
      return

    logger.info("执行工具: %s", tool)
    result = execute_tool(tool, params, self.permissions, self.whitelist_dirs)

    # 发送响应
    response = {"id": msg_id}
    if "result" in result:
      response["result"] = result["result"]
    elif "error" in result:
      response["error"] = result["error"]

    await ws.send(json.dumps(response))
