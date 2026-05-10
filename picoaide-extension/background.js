// PicoAide Helper — Browser MCP 工具执行
// 通过 WebSocket 接收 Go Relay 的工具命令，用 Chrome Extension API 执行

// ─── 配置常量 ─────────────────────────────────────────────────────────────────
const CONFIG = {
  connectionTimeout: 5000,
  connectTimeout: 15000,
  retryDelays: [0, 100, 200, 400, 800],
  keepaliveInterval: 20000,
};

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

function waitForTabComplete(tabId, timeoutMs = CONFIG.connectTimeout) {
  return new Promise(resolve => {
    const timeout = setTimeout(() => {
      chrome.tabs.onUpdated.removeListener(listener);
      resolve({ success: true });
    }, timeoutMs);
    const listener = (updatedTabId, changeInfo) => {
      if (updatedTabId === tabId && changeInfo.status === 'complete') {
        clearTimeout(timeout);
        chrome.tabs.onUpdated.removeListener(listener);
        resolve({ success: true });
      }
    };
    chrome.tabs.onUpdated.addListener(listener);
  });
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
  const delays = CONFIG.retryDelays;
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
  browser_go_forward: handleGoForward,
  browser_reload: handleReload,
  browser_current_tab: handleCurrentTab,
  browser_tab_select: handleTabSelect,
  browser_scroll: handleScroll,
  browser_key_press: handleKeyPress,
  browser_get_attribute: handleGetAttribute,
  browser_get_links: handleGetLinks,
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
  return await waitForTabComplete(currentTabId);
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
      id: t.id,
      url: t.url,
      title: t.title,
      active: t.active,
      current: t.id === currentTabId,
      windowId: t.windowId,
      index: t.index,
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

async function handleGoForward() {
  if (!currentTabId) throw new Error('没有活动的标签页');
  return await executeContentScript(currentTabId,
    () => { history.forward(); return { success: true }; }
  );
}

async function handleReload(params) {
  if (!currentTabId) throw new Error('没有活动的标签页');
  await chrome.tabs.reload(currentTabId, { bypassCache: !!params.bypassCache });
  return await waitForTabComplete(currentTabId);
}

async function handleCurrentTab() {
  if (!currentTabId) throw new Error('没有活动的标签页');
  const tab = await chrome.tabs.get(currentTabId);
  return {
    tab: {
      id: tab.id,
      url: tab.url,
      title: tab.title,
      active: tab.active,
      windowId: tab.windowId,
      index: tab.index,
      status: tab.status,
    }
  };
}

async function handleTabSelect(params) {
  const tabId = Number(params.tabId);
  if (!Number.isInteger(tabId)) throw new Error('tabId 必须是整数');
  const tab = await chrome.tabs.get(tabId);
  await chrome.tabs.update(tabId, { active: true });
  if (tab.windowId !== undefined && chrome.windows?.update) {
    try { await chrome.windows.update(tab.windowId, { focused: true }); } catch {}
  }
  setCurrentTabId(tabId);
  addTabToGroup(tabId).catch(() => {});
  updateTabBadge(tabId).catch(() => {});
  return { success: true, tabId };
}

async function handleScroll(params) {
  if (!currentTabId) throw new Error('没有活动的标签页');
  return await executeContentScript(currentTabId,
    (selector, x, y) => {
      const dx = Number(x) || 0;
      const dy = Number(y) || 0;
      if (selector) {
        const el = document.querySelector(selector);
        if (!el) throw new Error('找不到元素: ' + selector);
        el.scrollBy({ left: dx, top: dy, behavior: 'instant' });
        return { success: true, scrollLeft: el.scrollLeft, scrollTop: el.scrollTop };
      }
      window.scrollBy({ left: dx, top: dy, behavior: 'instant' });
      return { success: true, scrollX: window.scrollX, scrollY: window.scrollY };
    },
    params.selector || '', params.x || 0, params.y || 0
  );
}

async function handleKeyPress(params) {
  if (!currentTabId) throw new Error('没有活动的标签页');
  if (!params.key) throw new Error('key 不能为空');
  return await executeContentScript(currentTabId,
    (selector, key, modifiers) => {
      const target = selector ? document.querySelector(selector) : (document.activeElement || document.body);
      if (!target) throw new Error(selector ? '找不到元素: ' + selector : '找不到可接收按键的元素');
      if (target.focus) target.focus();

      const eventInit = {
        key,
        code: key.length === 1 ? 'Key' + key.toUpperCase() : key,
        bubbles: true,
        cancelable: true,
        composed: true,
        ...modifiers,
      };
      const down = new KeyboardEvent('keydown', eventInit);
      const press = new KeyboardEvent('keypress', eventInit);
      const up = new KeyboardEvent('keyup', eventInit);
      target.dispatchEvent(down);
      target.dispatchEvent(press);
      target.dispatchEvent(up);

      if (key === 'Enter') {
        if (target.tagName === 'FORM') target.requestSubmit?.();
        else target.closest?.('form')?.requestSubmit?.();
      }
      return { success: true };
    },
    params.selector || '', params.key, {
      ctrlKey: !!params.ctrlKey,
      shiftKey: !!params.shiftKey,
      altKey: !!params.altKey,
      metaKey: !!params.metaKey,
    }
  );
}

async function handleGetAttribute(params) {
  if (!currentTabId) throw new Error('没有活动的标签页');
  return await executeContentScript(currentTabId,
    (selector, name) => {
      const el = document.querySelector(selector);
      if (!el) throw new Error('找不到元素: ' + selector);
      const attrValue = el.getAttribute(name);
      const propValue = el[name];
      const propType = typeof propValue;
      const serializablePropValue = propValue == null || ['string', 'number', 'boolean'].includes(propType)
        ? propValue
        : String(propValue);
      return {
        name,
        value: attrValue !== null ? attrValue : serializablePropValue,
        attributeValue: attrValue,
        propertyValue: serializablePropValue,
        propertyType: propType,
      };
    },
    params.selector, params.name
  );
}

async function handleGetLinks(params) {
  if (!currentTabId) throw new Error('没有活动的标签页');
  const limit = params.limit || 100;
  return await executeContentScript(currentTabId,
    (selector, maxLinks) => {
      const root = selector ? document.querySelector(selector) : document;
      if (!root) throw new Error('找不到元素: ' + selector);
      const links = Array.from(root.querySelectorAll('a[href]'))
        .slice(0, Math.max(0, Number(maxLinks) || 100))
        .map(a => ({
          text: (a.innerText || a.textContent || '').trim(),
          href: a.href,
          title: a.title || '',
          target: a.target || '',
        }));
      return { links };
    },
    params.selector || '', limit
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

  // 等待 WebSocket 连接建立
  const openPromise = new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      chrome.runtime.onMessage.removeListener(handler);
      reject(new Error('连接超时(5s)'));
    }, CONFIG.connectionTimeout);

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
  sendToOffscreen('offscreen-connect', { url: wsUrl });
  await openPromise;

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
    if (_restoreDone) {
      sendResponse({
        active,
        connectedTabIds: currentTabId ? [currentTabId] : [],
        tabCount: currentTabId ? 1 : 0,
      });
      return false;
    }
    // 等待状态恢复完成
    _restoreP.then(() => {
      sendResponse({
        active,
        connectedTabIds: currentTabId ? [currentTabId] : [],
        tabCount: currentTabId ? 1 : 0,
      });
    });
    return true;
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

// ─── Service Worker 存活保持 ────────────────────────────────────────────────────

// offscreen 通过 chrome.runtime.connect 建立长连接端口，保持 Service Worker 存活
chrome.runtime.onConnect.addListener((port) => {
  if (port.name === 'sw-keepalive') {
    // 端口存在期间 Service Worker 不会被杀
    port.onDisconnect.addListener(() => {});
  }
});

// ─── Service Worker 启动时恢复状态 ──────────────────────────────────────────────

// 恢复状态 Promise，cdpStatus 等待它完成后再响应
let _restoreDone = false;
let _restoreResolve;
const _restoreP = new Promise(r => { _restoreResolve = r; });

async function restoreState() {
  try {
    // 检查 offscreen document 是否存在
    const contexts = await chrome.runtime.getContexts({
      contextTypes: ['OFFSCREEN_DOCUMENT'],
      documentUrls: [chrome.runtime.getURL('offscreen.html')]
    });
    if (contexts.length === 0) return;

    // 检查 WebSocket 是否仍连接
    const resp = await chrome.runtime.sendMessage({ type: 'offscreen-status' });
    if (resp && resp.connected) {
      active = true;
      updateBadgeConnected();
      if (currentTabId) {
        addTabToGroup(currentTabId).catch(() => {});
        updateTabBadge(currentTabId).catch(() => {});
      }
    }
  } catch {}
}

(async () => {
  await restoreState();
  if (!active) cleanupStaleGroups().catch(() => {});
  _restoreDone = true;
  _restoreResolve();
})();
