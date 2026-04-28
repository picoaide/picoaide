// PicoAide Helper — background service worker

let cdpActive = false;
let cdpTabId = null;
let cdpWs = null;
let cdpTabInfo = null;

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg.action === 'cdpStatus') {
    sendResponse({
      active: cdpActive,
      tabId: cdpTabId,
      tabTitle: cdpTabInfo?.title || '',
      tabUrl: cdpTabInfo?.url || ''
    });
    return false;
  }
  if (msg.action === 'cdpToggle') {
    if (cdpActive) {
      cdpDisable().then(() => sendResponse({ active: false }));
    } else {
      cdpEnable().then(
        () => sendResponse({ active: true, error: null }),
        (err) => sendResponse({ active: false, error: err.message })
      );
    }
    return true;
  }
  if (msg.action === 'cdpForceCleanup') {
    cdpDisable().then(() => sendResponse({ done: true }));
    return true;
  }
});

async function cdpEnable() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab || !tab.url) throw new Error('无法获取当前标签页');
  if (!tab.url.startsWith('http://') && !tab.url.startsWith('https://')) {
    throw new Error('请在普通网页上使用此功能');
  }

  cdpTabId = tab.id;
  cdpTabInfo = { title: tab.title, url: tab.url };

  // 先清理可能残留的 debugger
  try { await chrome.debugger.detach({ tabId: cdpTabId }); } catch {}

  try {
    await chrome.debugger.attach({ tabId: cdpTabId }, '1.3');
  } catch (e) {
    cdpTabId = null;
    cdpTabInfo = null;
    throw new Error('无法附加调试器: ' + e.message);
  }

  // debugger 已附加，后续失败需要清理
  const { serverUrl } = await chrome.storage.local.get('serverUrl');
  const base = (serverUrl || '').replace(/\/+$/, '');
  if (!base) {
    await chrome.debugger.detach({ tabId: cdpTabId });
    cdpTabId = null;
    cdpTabInfo = null;
    throw new Error('未设置服务器地址');
  }

  // 获取 MCP token（通过 fetch 带 cookie）
  let mcpToken;
  try {
    const resp = await fetch(base + '/api/mcp/token', { credentials: 'include' });
    const data = await resp.json();
    if (!data.success) throw new Error(data.error || '获取 token 失败');
    mcpToken = data.token;
  } catch (e) {
    await chrome.debugger.detach({ tabId: cdpTabId });
    cdpTabId = null;
    cdpTabInfo = null;
    throw new Error('获取 MCP token 失败: ' + e.message);
  }

  const wsUrl = base.replace(/^http/, 'ws') + '/api/mcp/cdp?kind=browser&token=' + encodeURIComponent(mcpToken);
  let ws;
  try {
    ws = new WebSocket(wsUrl);
  } catch (e) {
    await chrome.debugger.detach({ tabId: cdpTabId });
    cdpTabId = null;
    cdpTabInfo = null;
    throw new Error('WebSocket 创建失败: ' + e.message);
  }

  // 等待连接建立
  try {
    await new Promise((resolve, reject) => {
      ws.onopen = resolve;
      ws.onerror = () => reject(new Error('无法连接到服务器'));
      setTimeout(() => reject(new Error('连接超时(5s)')), 5000);
    });
  } catch (e) {
    try { ws.close(); } catch {}
    await chrome.debugger.detach({ tabId: cdpTabId });
    cdpTabId = null;
    cdpTabInfo = null;
    throw e;
  }

  cdpWs = ws;
  cdpActive = true;
  updateBadge('on');

  // CDP 命令路由
  const BROWSER_COMMANDS = {
    'Target.getTargets': (msg) => ({
      id: msg.id, result: { targetInfos: [{
        targetId: String(cdpTabId), type: 'page',
        title: cdpTabInfo.title || '', url: cdpTabInfo.url || '', attached: true,
      }]}
    }),
    'Target.attachToTarget': (msg) => ({ id: msg.id, result: { sessionId: 's' } }),
    'Target.detachFromTarget': (msg) => ({ id: msg.id, result: {} }),
    'Target.createTarget': (msg) => ({ id: msg.id, result: { targetId: String(cdpTabId) } }),
    'Target.closeTarget': (msg) => ({ id: msg.id, result: { success: true } }),
    'Target.activateTarget': (msg) => ({ id: msg.id, result: {} }),
  };

  cdpWs.onmessage = async (event) => {
    let msg;
    try { msg = JSON.parse(event.data); } catch { return; }
    if (!msg.id) return;

    const handler = BROWSER_COMMANDS[msg.method];
    if (handler) {
      safeSend(cdpWs, JSON.stringify(handler(msg)));
      return;
    }
    try {
      const result = await chrome.debugger.sendCommand({ tabId: cdpTabId }, msg.method, msg.params || {});
      safeSend(cdpWs, JSON.stringify({ id: msg.id, result: result || {} }));
    } catch (e) {
      safeSend(cdpWs, JSON.stringify({ id: msg.id, error: { message: e.message || String(e) } }));
    }
  };

  cdpWs.onclose = () => cdpDisable();
  cdpWs.onerror = () => {};

  chrome.debugger.onEvent.addListener(debuggerEventHandler);
}

function safeSend(ws, data) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    try { ws.send(data); } catch {}
  }
}

function debuggerEventHandler(source, method, params) {
  if (source.tabId === cdpTabId && cdpWs && cdpWs.readyState === WebSocket.OPEN) {
    safeSend(cdpWs, JSON.stringify({ method, params }));
  }
}

async function cdpDisable() {
  if (cdpTabId) {
    try { await chrome.debugger.detach({ tabId: cdpTabId }); } catch {}
  }
  if (cdpWs) { try { cdpWs.close(); } catch {} cdpWs = null; }
  try { chrome.debugger.onEvent.removeListener(debuggerEventHandler); } catch {}
  cdpTabId = null;
  cdpTabInfo = null;
  cdpActive = false;
  updateBadge('off');
}

function updateBadge(state) {
  const text = state === 'on' ? 'ON' : '';
  const color = state === 'on' ? '#4CAF50' : '#9E9E9E';
  chrome.action.setBadgeText({ text });
  chrome.action.setBadgeBackgroundColor({ color });
}

chrome.tabs.onRemoved.addListener((tabId) => {
  if (tabId === cdpTabId) cdpDisable();
});

chrome.debugger.onDetach.addListener((source) => {
  if (source.tabId === cdpTabId) cdpDisable();
});
