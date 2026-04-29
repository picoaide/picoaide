// PicoAide Helper — Browser MCP 工具执行
// 通过 WebSocket 接收 Go Relay 的工具命令，用 Chrome Extension API 执行

// ─── 全局状态 ─────────────────────────────────────────────────────────────────

let currentTabId = null;            // 当前控制的标签页 ID
let groupId = null;                 // Chrome 标签组 ID
const groupTabIds = new Set();      // 标签组成员集合
let active = false;                 // 连接是否激活
let eventListeners = [];            // chrome.* 事件监听器引用

// Service Worker 可能被终止重启，从 session storage 恢复 currentTabId
chrome.storage.session.get('currentTabId').then(r => {
  if (r.currentTabId) currentTabId = r.currentTabId;
}).catch(() => {});

function setCurrentTabId(tabId) {
  currentTabId = tabId;
  chrome.storage.session.set({ currentTabId: tabId }).catch(() => {});
}

// ─── 常量 ─────────────────────────────────────────────────────────────────────

const GROUP_TITLE = 'PicoAide';
const GROUP_COLOR = 'green';
const NON_DEBUGGABLE_SCHEMES = ['chrome:', 'edge:', 'devtools:'];

// ─── Offscreen Document 管理 ──────────────────────────────────────────────────

async function ensureOffscreenDocument() {
  const existingContexts = await chrome.runtime.getContexts({
    contextTypes: ['OFFSCREEN_DOCUMENT'],
    documentUrls: [chrome.runtime.getURL('offscreen.html')]
  });
  if (existingContexts.length > 0) {
    return;
  }
  await chrome.offscreen.createDocument({
    url: 'offscreen.html',
    reasons: ['WEB_RTC'],
    justification: 'WebSocket connection to relay server',
  });
}

async function closeOffscreenDocument() {
  try {
    await chrome.offscreen.closeDocument();
  } catch {}
}

function sendToOffscreen(type, data) {
  chrome.runtime.sendMessage({ type, ...data }).catch(() => {});
}

// ─── 工具函数 ─────────────────────────────────────────────────────────────────

function isNonDebuggableUrl(url) {
  return !!url && NON_DEBUGGABLE_SCHEMES.some(s => url.startsWith(s));
}

// 在标签页中执行 content script
async function executeContentScript(tabId, func, ...args) {
  const results = await chrome.scripting.executeScript({
    target: { tabId },
    func,
    args,
  });
  if (results[0]?.error) {
    throw new Error(results[0].error.message || 'Content script error');
  }
  return results[0]?.result;
}

// ─── 标签组管理 ───────────────────────────────────────────────────────────────

async function addTabToGroup(tabId) {
  if (groupTabIds.has(tabId)) return;
  try {
    await retryOnDrag(async () => {
      if (groupId === null) {
        groupId = await chrome.tabs.group({ tabIds: [tabId] });
        await chrome.tabGroups.update(groupId, { color: GROUP_COLOR, title: GROUP_TITLE });
      } else {
        await chrome.tabs.group({ groupId, tabIds: [tabId] });
      }
    });
    groupTabIds.add(tabId);
  } catch (e) {
    console.error('[PicoAide] 加入标签组失败:', e);
  }
}

async function ungroupAll() {
  const tabIds = [...groupTabIds];
  groupTabIds.clear();
  groupId = null;
  if (tabIds.length) {
    try { await retryOnDrag(() => chrome.tabs.ungroup(tabIds)); } catch {}
  }
}

async function retryOnDrag(fn) {
  const delays = [0, 100, 200, 400, 800];
  let lastError;
  for (const delay of delays) {
    if (delay) await new Promise(r => setTimeout(r, delay));
    try { await fn(); return; } catch (e) {
      if (!e?.message?.includes('user may be dragging a tab')) throw e;
      lastError = e;
    }
  }
  throw lastError;
}

async function cleanupStaleGroups() {
  try {
    const groups = await chrome.tabGroups.query({ title: GROUP_TITLE });
    const tabsPerGroup = await Promise.all(groups.map(g => chrome.tabs.query({ groupId: g.id })));
    const tabIds = tabsPerGroup.flat().map(t => t.id).filter(id => id !== undefined);
    if (tabIds.length) await chrome.tabs.ungroup(tabIds);
  } catch {}
}

// ─── 徽章管理 ─────────────────────────────────────────────────────────────────

function updateBadgeConnected() {
  chrome.action.setBadgeText({ text: 'ON' });
  chrome.action.setBadgeBackgroundColor({ color: '#4CAF50' });
}

function updateBadgeOff() {
  chrome.action.setBadgeText({ text: '' });
  // 清除特定标签页的徽章覆盖（updateTabBadge 设置的）
  if (currentTabId) {
    chrome.action.setBadgeText({ tabId: currentTabId, text: '' }).catch(() => {});
  }
}

async function updateTabBadge(tabId) {
  try {
    await chrome.action.setBadgeText({ tabId, text: 'ON' });
    await chrome.action.setBadgeBackgroundColor({ tabId, color: '#4CAF50' });
  } catch {}
}

// ─── 工具处理器 ───────────────────────────────────────────────────────────────

const TOOL_HANDLERS = {
  browser_navigate: handleNavigate,
  browser_screenshot: handleScreenshot,
  browser_click: handleClick,
  browser_type: handleType,
  browser_get_content: handleGetContent,
  browser_execute: handleExecute,
  browser_tabs_list: handleTabsList,
  browser_tab_new: handleTabNew,
  browser_tab_close: handleTabClose,
  browser_go_back: handleGoBack,
  browser_wait: handleWait,
};

async function handleToolCommand(msg) {
  const handler = TOOL_HANDLERS[msg.tool];
  if (!handler) {
    sendToOffscreen('offscreen-send', {
      data: JSON.stringify({ id: msg.id, error: { message: '未知工具: ' + msg.tool } })
    });
    return;
  }
  try {
    const result = await handler(msg.params || {});
    sendToOffscreen('offscreen-send', {
      data: JSON.stringify({ id: msg.id, result })
    });
  } catch (e) {
    sendToOffscreen('offscreen-send', {
      data: JSON.stringify({ id: msg.id, error: { message: e.message || String(e) } })
    });
  }
}

async function handleNavigate(params) {
  if (!currentTabId) throw new Error('没有活动的标签页');
  await chrome.tabs.update(currentTabId, { url: params.url });
  // 等待页面加载完成
  return new Promise(resolve => {
    const timeout = setTimeout(() => {
      chrome.tabs.onUpdated.removeListener(listener);
      resolve({ success: true });
    }, 15000);
    const listener = (tabId, changeInfo) => {
      if (tabId === currentTabId && changeInfo.status === 'complete') {
        clearTimeout(timeout);
        chrome.tabs.onUpdated.removeListener(listener);
        resolve({ success: true });
      }
    };
    chrome.tabs.onUpdated.addListener(listener);
  });
}

async function handleScreenshot() {
  if (!currentTabId) throw new Error('没有活动的标签页');
  const tab = await chrome.tabs.get(currentTabId);
  const dataUrl = await chrome.tabs.captureVisibleTab(tab.windowId, { format: 'png' });
  const base64 = dataUrl.replace(/^data:image\/png;base64,/, '');
  return {
    content: [{ type: 'image', data: base64, mimeType: 'image/png' }]
  };
}

async function handleClick(params) {
  if (!currentTabId) throw new Error('没有活动的标签页');
  return await executeContentScript(currentTabId,
    (selector) => {
      const el = document.querySelector(selector);
      if (!el) throw new Error('找不到元素: ' + selector);
      el.scrollIntoView({ block: 'center', behavior: 'instant' });
      el.click();
      return { success: true };
    },
    params.selector
  );
}

async function handleType(params) {
  if (!currentTabId) throw new Error('没有活动的标签页');
  return await executeContentScript(currentTabId,
    (selector, text) => {
      const el = document.querySelector(selector);
      if (!el) throw new Error('找不到元素: ' + selector);
      el.focus();
      el.dispatchEvent(new FocusEvent('focus', { bubbles: true }));
      // 选中输入框内已有文字
      if (el.setSelectionRange) {
        el.setSelectionRange(0, (el.value || '').length);
      }
      // 尝试用 execCommand 模拟键盘输入
      const ok = document.execCommand('insertText', false, text);
      // execCommand 失败时回退到原生 setter + 事件
      if (!ok || !el.value) {
        const setter = Object.getOwnPropertyDescriptor(
          HTMLInputElement.prototype, 'value'
        ).set;
        setter.call(el, text);
        el.dispatchEvent(new Event('input', { bubbles: true }));
        el.dispatchEvent(new Event('change', { bubbles: true }));
      }
      return { success: true };
    },
    params.selector, params.text
  );
}

async function handleGetContent(params) {
  if (!currentTabId) throw new Error('没有活动的标签页');
  const selector = params.selector || 'body';
  return await executeContentScript(currentTabId,
    (sel) => {
      const el = document.querySelector(sel);
      if (!el) return { content: '' };
      // input/textarea 用 value，其他元素用 innerText
      if (typeof el.value === 'string') return { content: el.value };
      return { content: el.innerText };
    },
    selector
  );
}

async function handleExecute(params) {
  if (!currentTabId) throw new Error('没有活动的标签页');
  const results = await chrome.scripting.executeScript({
    target: { tabId: currentTabId },
    world: 'MAIN',
    func: (code) => {
      try { return eval(code); }
      catch (e) { return { error: e.message }; }
    },
    args: [params.script],
  });
  return { result: results[0]?.result };
}

async function handleTabsList() {
  const tabs = await chrome.tabs.query({});
  return {
    tabs: tabs.map(t => ({
      id: t.id, url: t.url, title: t.title, active: t.active
    }))
  };
}

async function handleTabNew(params) {
  const tab = await chrome.tabs.create(params.url ? { url: params.url } : {});
  setCurrentTabId(tab.id);
  addTabToGroup(tab.id).catch(() => {});
  return { tabId: tab.id };
}

async function handleTabClose(params) {
  await chrome.tabs.remove(params.tabId);
  if (currentTabId === params.tabId) setCurrentTabId(null);
  return { success: true };
}

async function handleGoBack() {
  if (!currentTabId) throw new Error('没有活动的标签页');
  return await executeContentScript(currentTabId,
    () => { history.back(); return { success: true }; }
  );
}

async function handleWait(params) {
  if (!currentTabId) throw new Error('没有活动的标签页');
  const timeout = params.timeout || 10000;
  return await executeContentScript(currentTabId,
    (selector, timeoutMs) => {
      return new Promise((resolve, reject) => {
        const el = document.querySelector(selector);
        if (el) { resolve({ found: true }); return; }
        const observer = new MutationObserver(() => {
          if (document.querySelector(selector)) {
            observer.disconnect();
            clearTimeout(timer);
            resolve({ found: true });
          }
        });
        observer.observe(document.body, { childList: true, subtree: true });
        const timer = setTimeout(() => {
          observer.disconnect();
          reject(new Error('等待超时: ' + selector));
        }, timeoutMs);
      });
    },
    params.selector, timeout
  );
}

// ─── Offscreen 消息处理 ──────────────────────────────────────────────────────

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  // 来自 offscreen document 的消息（WebSocket 收到的数据）
  if (msg.type === 'offscreen-message') {
    let message;
    try { message = JSON.parse(msg.data); } catch { return false; }

    // 工具命令：Go Relay → Extension
    if (message.id !== undefined && message.tool) {
      handleToolCommand(message);
      return false;
    }
    return false;
  }

  if (msg.type === 'offscreen-open') {
    console.log('[PicoAide] WebSocket 已连接');
    return false;
  }

  if (msg.type === 'offscreen-close') {
    console.log('[PicoAide] WebSocket 关闭, code=' + msg.code);
    if (active) cdpDisable();
    return false;
  }

  if (msg.type === 'offscreen-error') {
    console.error('[PicoAide] WebSocket 错误:', msg.error);
    return false;
  }

  return false;
});

// ─── 连接生命周期 ─────────────────────────────────────────────────────────────

async function cdpEnable() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab || !tab.id) throw new Error('无法获取当前标签页');
  if (!tab.url || isNonDebuggableUrl(tab.url)) {
    if (!tab.url || (!tab.url.startsWith('http://') && !tab.url.startsWith('https://')))
      throw new Error('请在普通网页上使用此功能');
    throw new Error('无法在 chrome:// 等特殊页面上使用此功能');
  }

  const { serverUrl } = await chrome.storage.local.get('serverUrl');
  const base = (serverUrl || '').replace(/\/+$/, '');
  if (!base) throw new Error('未设置服务器地址');

  let mcpToken;
  try {
    const resp = await fetch(base + '/api/mcp/token', { credentials: 'include' });
    const data = await resp.json();
    if (!data.success) throw new Error(data.error || '获取 token 失败');
    mcpToken = data.token;
  } catch (e) {
    throw new Error('获取 MCP token 失败: ' + e.message);
  }

  // 断线重连时保留 currentTabId
  if (!currentTabId) {
    setCurrentTabId(tab.id);
  }

  const wsUrl = base.replace(/^http/, 'ws') + '/api/browser/ws?token=' + encodeURIComponent(mcpToken);

  await ensureOffscreenDocument();
  sendToOffscreen('offscreen-connect', { url: wsUrl });

  // 等待 WebSocket 连接建立
  await new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      chrome.runtime.onMessage.removeListener(handler);
      reject(new Error('连接超时(5s)'));
    }, 5000);

    function handler(msg) {
      if (msg.type === 'offscreen-open') {
        clearTimeout(timeout);
        chrome.runtime.onMessage.removeListener(handler);
        resolve();
      } else if (msg.type === 'offscreen-error') {
        clearTimeout(timeout);
        chrome.runtime.onMessage.removeListener(handler);
        reject(new Error(msg.error || 'WebSocket 连接失败'));
      }
    }
    chrome.runtime.onMessage.addListener(handler);
  });

  active = true;

  await cleanupStaleGroups();
  addTabToGroup(currentTabId).catch(() => {});
  updateTabBadge(currentTabId).catch(() => {});

  updateBadgeConnected();
}

async function cdpDisable() {
  if (!active) return;
  active = false;
  sendToOffscreen('offscreen-disconnect', {});
  await ungroupAll();
  updateBadgeOff();
}

// ─── popup.js 消息处理 ────────────────────────────────────────────────────────

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (msg.action === 'cdpStatus') {
    sendResponse({
      active,
      connectedTabIds: currentTabId ? [currentTabId] : [],
      tabCount: currentTabId ? 1 : 0,
    });
    return false;
  }

  if (msg.action === 'cdpToggle') {
    if (active) {
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

  return false;
});

// ─── Service Worker 启动时清理残留标签组 ────────────────────────────────────────

cleanupStaleGroups().catch(() => {});
