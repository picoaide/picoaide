// PicoAide Helper — Browser MCP 工具执行
// 通过 WebSocket 接收 Go Relay 的工具命令，用 Chrome Debugger API 后台执行

// ─── 配置常量 ─────────────────────────────────────────────────────────────────
const CONFIG = {
  connectionTimeout: 5000,
  connectTimeout: 15000,
  retryDelays: [0, 100, 200, 400, 800],
  keepaliveInterval: 20000,
  reconnectBaseDelay: 1000,
  reconnectMaxDelay: 30000,
  reconnectMaxAttempts: 50,
  debuggerVersion: '1.3',
};

// ─── 全局状态 ─────────────────────────────────────────────────────────────────

let currentTabId = null;
let groupId = null;
const groupTabIds = new Set();
let active = false;
let debuggerAttached = false;

// 连接状态：'connected' | 'reconnecting' | 'disconnected'
let connectionState = 'disconnected';

// 自动重连状态（持久化到 session storage 以跨越 SW 重启）
let reconnectTimer = null;
let reconnectAttempt = 0;
let reconnectUrl = null;
let reconnectServerBase = null;
let wsReplacing = false; // 正在替换旧 WS（connectWebSocket 中），屏蔽旧 WS 的 close 事件
let wsGraceUntil = 0;    // 新 WS 连上后的宽限期（毫秒时间戳），期内忽略 close 事件

// 重连状态恢复
let reconnectStateReady = false;
let savedMcpToken = ''; // 持久化的 MCP token，重连时复用避免 session cookie 依赖
const reconnectStatePromise = new Promise(resolve => {
  chrome.storage.session.get(['currentTabId', 'reconnectServerBase', 'reconnectUrl', 'reconnectAttempt', 'mcpToken']).then(r => {
    if (r.currentTabId) currentTabId = r.currentTabId;
    if (r.reconnectServerBase) reconnectServerBase = r.reconnectServerBase;
    if (r.reconnectUrl) reconnectUrl = r.reconnectUrl;
    if (r.reconnectAttempt) reconnectAttempt = r.reconnectAttempt;
    if (r.mcpToken) savedMcpToken = r.mcpToken;
    reconnectStateReady = true;
    resolve();
  }).catch(() => { reconnectStateReady = true; resolve(); });
});

reconnectStatePromise.then(async () => {
  await restorePromise; // 等 restoreState 确认 offscreen 是否还活着
  if (!active && reconnectServerBase && currentTabId) {
    connectionState = 'reconnecting';
    scheduleReconnect();
  }
});

function setCurrentTabId(tabId) {
  currentTabId = tabId;
  chrome.storage.session.set({ currentTabId: tabId }).catch(() => {});
}

function saveReconnectState() {
  chrome.storage.session.set({
    reconnectServerBase: reconnectServerBase || '',
    reconnectUrl: reconnectUrl || '',
    reconnectAttempt: reconnectAttempt,
    mcpToken: savedMcpToken || '',
  }).catch(() => {});
}

function clearReconnectState() {
  chrome.storage.session.remove(['reconnectServerBase', 'reconnectUrl', 'reconnectAttempt', 'mcpToken']).catch(() => {});
}

const GROUP_TITLE = 'PicoAide';
const GROUP_COLOR = 'green';
const NON_DEBUGGABLE_SCHEMES = ['chrome:', 'edge:', 'devtools:'];

// ─── Offscreen Document 管理 ──────────────────────────────────────────────────

async function ensureOffscreenDocument() {
  const existingContexts = await chrome.runtime.getContexts({
    contextTypes: ['OFFSCREEN_DOCUMENT'],
    documentUrls: [chrome.runtime.getURL('offscreen.html')],
  });
  if (existingContexts.length > 0) return;
  await chrome.offscreen.createDocument({
    url: 'offscreen.html',
    reasons: ['WEB_RTC'],
    justification: 'WebSocket connection to relay server',
  });
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

// 检查标签页是否存在
async function ensureTabExists() {
  if (!currentTabId) throw new Error('没有活动的标签页');
  try {
    await chrome.tabs.get(currentTabId);
  } catch {
    setCurrentTabId(null);
    throw new Error('标签页已关闭');
  }
}

// 确保 Debugger 已附加（导航后可能断开，自动重连）
async function ensureDebugger() {
  if (!currentTabId) throw new Error('没有活动的标签页');
  if (debuggerAttached) return;
  await attachDebugger(currentTabId);
}

// 通过 Debugger Runtime.evaluate 在标签页中执行 JS（不激活标签页）
async function evaluateInTab(fnOrExpr, ...args) {
  await ensureTabExists();
  await ensureDebugger();

  const isFn = typeof fnOrExpr === 'function';
  const expression = isFn
    ? `(${fnOrExpr.toString()}).apply(null, ${JSON.stringify(args)})`
    : fnOrExpr;

  const result = await chrome.debugger.sendCommand(
    { tabId: currentTabId },
    'Runtime.evaluate',
    { expression, returnByValue: true, awaitPromise: true },
  );

  if (result.exceptionDetails) {
    const msg = result.exceptionDetails.text
      || result.exceptionDetails.exception?.description
      || '执行错误';
    throw new Error(msg.replace(/^Uncaught\s/, ''));
  }
  return result.result.value;
}

// ─── Debugger 生命周期 ────────────────────────────────────────────────────────

function attachDebugger(tabId) {
  return new Promise((resolve, reject) => {
    chrome.debugger.attach({ tabId }, CONFIG.debuggerVersion, () => {
      if (chrome.runtime.lastError) {
        const msg = chrome.runtime.lastError.message;
        // 如果 debugger 已被其他上下文附加（如 Chrome DevTools），先 detach 再重试
        if (msg.includes('already attached')) {
          chrome.debugger.detach({ tabId }, () => {
            chrome.debugger.attach({ tabId }, CONFIG.debuggerVersion, () => {
              if (chrome.runtime.lastError) {
                reject(new Error(chrome.runtime.lastError.message));
                return;
              }
              debuggerAttached = true;
              resolve();
            });
          });
        } else {
          reject(new Error(msg));
        }
        return;
      }
      debuggerAttached = true;
      resolve();
    });
  });
}

function detachDebugger(tabId) {
  return new Promise(resolve => {
    if (!debuggerAttached) { resolve(); return; }
    chrome.debugger.detach({ tabId }, () => {
      debuggerAttached = false;
      resolve();
    });
  });
}

// Debugger 被 Chrome 主动断开（如标签页关闭/导航到新页面）
chrome.debugger.onDetach.addListener((source) => {
  if (source.tabId === currentTabId) {
    debuggerAttached = false;
  }
});

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
    console.error('[PicoAide] 标签组失败:', e);
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
  connectionState = 'connected';
  chrome.action.setBadgeText({ text: 'ON' });
  chrome.action.setBadgeBackgroundColor({ color: '#4CAF50' });
}

function updateBadgeReconnecting(attempt) {
  connectionState = 'reconnecting';
  chrome.action.setBadgeText({ text: attempt > 0 ? String(attempt) : 'R' });
  chrome.action.setBadgeBackgroundColor({ color: '#FF9800' });
}

function updateBadgeOff() {
  connectionState = 'disconnected';
  chrome.action.setBadgeText({ text: '' });
}

// ─── 自动重连 ─────────────────────────────────────────────────────────────────

function stopReconnect() {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  reconnectAttempt = 0;
  clearReconnectState();
}

async function scheduleReconnect() {
  // 如果内存中还没有重连信息，尝试从 storage 恢复（SW 重启时可能落后于消息）
  if (!reconnectServerBase) {
    await reconnectStatePromise;
  }
  if (!reconnectServerBase) return; // 确实没有重连信息

  reconnectAttempt++;
  saveReconnectState();
  updateBadgeReconnecting(reconnectAttempt);
  if (reconnectAttempt > CONFIG.reconnectMaxAttempts) {
    console.log('[PicoAide] 重连次数已达上限，停止重试');
    await fullDisconnect();
    return;
  }

  // 指数退避 + 随机抖动
  const delay = Math.min(
    CONFIG.reconnectBaseDelay * Math.pow(2, reconnectAttempt - 1),
    CONFIG.reconnectMaxDelay,
  ) + Math.random() * 1000;

  console.log(`[PicoAide] 将在 ${Math.round(delay)}ms 后自动重连 (第 ${reconnectAttempt} 次)`);

  reconnectTimer = setTimeout(async () => {
    reconnectTimer = null;
    try {
      await tryReconnect();
    } catch (e) {
      console.log('[PicoAide] 重连失败:', e.message);
      scheduleReconnect();
    }
  }, delay);
}

async function tryReconnect() {
  if (!reconnectServerBase || !currentTabId) return;

  console.log('[PicoAide] 尝试重连...');

  // 检查标签页是否还在
  try {
    const tab = await chrome.tabs.get(currentTabId);
    console.log('[PicoAide] 标签页存在:', tab.id, tab.url);
  } catch (e) {
    console.log('[PicoAide] 标签页已关闭，停止重连');
    await fullDisconnect();
    return;
  }

  // 重新附加 Debugger。如果 F12 开发者工具打开会导致附加失败，
  // 但不影响 WebSocket 连接和自动重连，仅影响后续工具调用。
  try {
    await attachDebugger(currentTabId);
    debuggerAttached = true;
    console.log('[PicoAide] Debugger 已附加');
  } catch (e) {
    console.error('[PicoAide] 附加 Debugger 失败(不影响重连):', e.message);
    debuggerAttached = false;
  }

  // 获取最新 MCP token（syncUser 可能重置了 token）
  let mcpToken = '';
  try {
    const resp = await fetch(reconnectServerBase + '/api/mcp/token', { credentials: 'include' });
    if (resp.ok) {
      const data = await resp.json();
      if (data.success) mcpToken = data.token;
    }
  } catch (e) {
    console.error('[PicoAide] 刷新 token 失败:', e.message);
  }
  if (!mcpToken) mcpToken = savedMcpToken; // fallback
  if (!mcpToken) throw new Error('无可用 token');
  savedMcpToken = mcpToken;
  saveReconnectState();

  const wsUrl = reconnectServerBase.replace(/^http/, 'ws') + '/api/browser/ws?token=' + encodeURIComponent(mcpToken);

  // 确保 offscreen document 存在
  await ensureOffscreenDocument();

  let wsHandler;
  const openPromise = new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      if (wsHandler) chrome.runtime.onMessage.removeListener(wsHandler);
      reject(new Error('重连超时'));
    }, CONFIG.connectionTimeout);

    wsHandler = function handler(msg) {
      if (msg.type === 'offscreen-open') {
        clearTimeout(timeout);
        if (wsHandler) chrome.runtime.onMessage.removeListener(wsHandler);
        console.log('[PicoAide] WebSocket 重连成功');
        resolve();
      } else if (msg.type === 'offscreen-error') {
        clearTimeout(timeout);
        if (wsHandler) chrome.runtime.onMessage.removeListener(wsHandler);
        console.error('[PicoAide] WebSocket 重连错误:', msg.error);
        reject(new Error(msg.error || 'WebSocket 重连失败'));
      }
    };
    chrome.runtime.onMessage.addListener(wsHandler);
  });

  try {
    wsReplacing = true;
    wsGraceUntil = Infinity; // 连接期间屏蔽所有旧 WS 的延迟 close 事件
    console.log('[PicoAide] 发送 offscreen-connect');
    sendToOffscreen('offscreen-connect', { url: wsUrl });
    await openPromise;
  } finally {
    wsReplacing = false;
    wsGraceUntil = 0; // 连接完成，后续 close 视为真实断开
    if (wsHandler) {
      chrome.runtime.onMessage.removeListener(wsHandler);
      wsHandler = null;
    }
  }

  reconnectAttempt = 0;
  saveReconnectState();
  active = true;
  console.log('[PicoAide] 自动重连成功');
  updateBadgeConnected();
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
      data: JSON.stringify({ id: msg.id, error: { message: '未知工具: ' + msg.tool } }),
    });
    return;
  }
  try {
    const result = await handler(msg.params || {});
    sendToOffscreen('offscreen-send', {
      data: JSON.stringify({ id: msg.id, result }),
    });
  } catch (e) {
    sendToOffscreen('offscreen-send', {
      data: JSON.stringify({ id: msg.id, error: { message: e.message || String(e) } }),
    });
  }
}

// navigate — Page.navigate 后台导航，不激活标签页
async function handleNavigate(params) {
  await ensureTabExists();
  await ensureDebugger();
  await chrome.debugger.sendCommand({ tabId: currentTabId }, 'Page.navigate', { url: params.url });
  return await waitForTabComplete(currentTabId);
}

// screenshot — Page.captureScreenshot 后台截图，不激活标签页
async function handleScreenshot() {
  await ensureTabExists();
  await ensureDebugger();
  const result = await chrome.debugger.sendCommand({ tabId: currentTabId }, 'Page.captureScreenshot', { format: 'png' });
  return { content: [{ type: 'image', data: result.data, mimeType: 'image/png' }] };
}

// click — Runtime.evaluate
async function handleClick(params) {
  return await evaluateInTab((selector) => {
    const el = document.querySelector(selector);
    if (!el) throw new Error('找不到元素: ' + selector);
    el.scrollIntoView({ block: 'center', behavior: 'instant' });
    el.click();
    return { success: true };
  }, params.selector);
}

// type — Runtime.evaluate 后台输入
async function handleType(params) {
  return await evaluateInTab((selector, text) => {
    const el = document.querySelector(selector);
    if (!el) throw new Error('找不到元素: ' + selector);
    el.focus();
    el.dispatchEvent(new FocusEvent('focus', { bubbles: true }));
    if (el.setSelectionRange) {
      el.setSelectionRange(0, (el.value || '').length);
    }
    const ok = document.execCommand('insertText', false, text);
    if (!ok || !el.value) {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
      setter.call(el, text);
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
    }
    return { success: true };
  }, params.selector, params.text);
}

// get_content — Runtime.evaluate
async function handleGetContent(params) {
  const selector = params.selector || 'body';
  return await evaluateInTab((sel) => {
    const el = document.querySelector(sel);
    if (!el) return { content: '' };
    if (typeof el.value === 'string') return { content: el.value };
    return { content: el.innerText };
  }, selector);
}

// execute — Runtime.evaluate
async function handleExecute(params) {
  const result = await evaluateInTab('(function(){ try { return ' + params.script + ' } catch(e) { return {error: e.message} } })()');
  return { result };
}

// 纯 tabs API，无需 debugger（不激活标签页）
async function handleTabsList() {
  const tabs = await chrome.tabs.query({});
  return {
    tabs: tabs.map(t => ({
      id: t.id, url: t.url, title: t.title, active: t.active,
      current: t.id === currentTabId, windowId: t.windowId, index: t.index,
    })),
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
  await ensureTabExists();
  await ensureDebugger();
  await chrome.debugger.sendCommand({ tabId: currentTabId }, 'Page.navigateToHistoryEntry', {
    entryId: (await chrome.debugger.sendCommand({ tabId: currentTabId }, 'Page.getNavigationHistory')).currentIndex - 1,
  });
  return await waitForTabComplete(currentTabId);
}

async function handleGoForward() {
  await ensureTabExists();
  await ensureDebugger();
  const history = await chrome.debugger.sendCommand({ tabId: currentTabId }, 'Page.getNavigationHistory');
  const entries = history.entries || [];
  const nextIdx = history.currentIndex + 1;
  if (nextIdx >= entries.length) throw new Error('没有前进页面');
  await chrome.debugger.sendCommand({ tabId: currentTabId }, 'Page.navigateToHistoryEntry', { entryId: nextIdx });
  return await waitForTabComplete(currentTabId);
}

// reload — Page.reload 后台刷新
async function handleReload(params) {
  await ensureTabExists();
  await ensureDebugger();
  await chrome.debugger.sendCommand({ tabId: currentTabId }, 'Page.reload', { ignoreCache: !!params.bypassCache });
  return await waitForTabComplete(currentTabId);
}

async function handleCurrentTab() {
  await ensureTabExists();
  const tab = await chrome.tabs.get(currentTabId);
  return {
    tab: { id: tab.id, url: tab.url, title: tab.title, active: tab.active, windowId: tab.windowId, index: tab.index, status: tab.status },
  };
}

// tab_select — 显式切换标签页（预期行为：跳转到该标签页）
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
  return { success: true, tabId };
}

// scroll — Runtime.evaluate
async function handleScroll(params) {
  return await evaluateInTab((selector, x, y) => {
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
  }, params.selector || '', params.x || 0, params.y || 0);
}

// key_press — Runtime.evaluate
async function handleKeyPress(params) {
  if (!params.key) throw new Error('key 不能为空');
  return await evaluateInTab((selector, key, modifiers) => {
    const target = selector ? document.querySelector(selector) : (document.activeElement || document.body);
    if (!target) throw new Error(selector ? '找不到元素: ' + selector : '找不到可接收按键的元素');
    if (target.focus) target.focus();
    const eventInit = {
      key, code: key.length === 1 ? 'Key' + key.toUpperCase() : key,
      bubbles: true, cancelable: true, composed: true, ...modifiers,
    };
    target.dispatchEvent(new KeyboardEvent('keydown', eventInit));
    target.dispatchEvent(new KeyboardEvent('keypress', eventInit));
    target.dispatchEvent(new KeyboardEvent('keyup', eventInit));
    if (key === 'Enter') {
      if (target.tagName === 'FORM') target.requestSubmit?.();
      else target.closest?.('form')?.requestSubmit?.();
    }
    return { success: true };
  }, params.selector || '', params.key, {
    ctrlKey: !!params.ctrlKey, shiftKey: !!params.shiftKey,
    altKey: !!params.altKey, metaKey: !!params.metaKey,
  });
}

// get_attribute — Runtime.evaluate
async function handleGetAttribute(params) {
  return await evaluateInTab((selector, name) => {
    const el = document.querySelector(selector);
    if (!el) throw new Error('找不到元素: ' + selector);
    const attrValue = el.getAttribute(name);
    const propValue = el[name];
    const propType = typeof propValue;
    const serializablePropValue = propValue == null || ['string', 'number', 'boolean'].includes(propType)
      ? propValue : String(propValue);
    return {
      name, value: attrValue !== null ? attrValue : serializablePropValue,
      attributeValue: attrValue, propertyValue: serializablePropValue, propertyType: propType,
    };
  }, params.selector, params.name);
}

// get_links — Runtime.evaluate
async function handleGetLinks(params) {
  const limit = params.limit || 100;
  return await evaluateInTab((selector, maxLinks) => {
    const root = selector ? document.querySelector(selector) : document;
    if (!root) throw new Error('找不到元素: ' + selector);
    const links = Array.from(root.querySelectorAll('a[href]'))
      .slice(0, Math.max(0, Number(maxLinks) || 100))
      .map(a => ({ text: (a.innerText || a.textContent || '').trim(), href: a.href, title: a.title || '', target: a.target || '' }));
    return { links };
  }, params.selector || '', limit);
}

// wait — Runtime.evaluate 用 Promise + MutationObserver（awaitPromise: true）
async function handleWait(params) {
  const timeout = params.timeout || 10000;
  return await evaluateInTab((selector, timeoutMs) => {
    return new Promise((resolve, reject) => {
      const el = document.querySelector(selector);
      if (el) { resolve({ found: true }); return; }
      const root = document.body || document.documentElement;
      if (!root) { reject(new Error('页面尚未准备好')); return; }
      const observer = new MutationObserver(() => {
        if (document.querySelector(selector)) {
          observer.disconnect();
          clearTimeout(timer);
          resolve({ found: true });
        }
      });
      observer.observe(root, { childList: true, subtree: true });
      const timer = setTimeout(() => {
        observer.disconnect();
        reject(new Error('等待超时: ' + selector));
      }, timeoutMs);
    });
  }, params.selector, timeout);
}

// ─── Offscreen 消息处理 ──────────────────────────────────────────────────────

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  // 来自 offscreen document 的消息（WebSocket 收到的数据）
  if (msg.type === 'offscreen-message') {
    let message;
    try { message = JSON.parse(msg.data); } catch { return false; }
    if (message.id !== undefined && message.tool) {
      handleToolCommand(message);
    }
    return false;
  }

  if (msg.type === 'offscreen-open') {
    console.log('[PicoAide] WebSocket 已连接');
    return false;
  }

  if (msg.type === 'offscreen-close') {
    console.log('[PicoAide] WebSocket 关闭, code=' + msg.code + ', clean=' + msg.wasClean);
    // 宽限期内忽略 close（新 WS 连上后旧 WS 的延迟事件）
    if (Date.now() < wsGraceUntil) {
      return false;
    }
    // 任何关闭都自动重连（fullDisconnect 会清 reconnectServerBase 阻止重连）
    scheduleReconnect();
    return false;
  }

  if (msg.type === 'offscreen-error') {
    console.error('[PicoAide] WebSocket 错误:', msg.error);
    return false;
  }

  // popup 消息
  if (msg.action === 'cdpStatus' || msg.action === 'cdpToggle' || msg.action === 'cdpForceCleanup') {
    handlePopupMessage(msg, sendResponse);
    return true;
  }

  return false;
});

// ─── popup 消息处理 ────────────────────────────────────────────────────────────

async function handlePopupMessage(msg, sendResponse) {
  switch (msg.action) {
    case 'cdpStatus':
      {
        await reconnectStatePromise;
        if (!restorePromiseDone) await restorePromise;
        let state = connectionState;
        if (!active && reconnectServerBase) {
          state = 'reconnecting';
        }
        sendResponse({ active, connectionState: state, connectedTabIds: currentTabId ? [currentTabId] : [], tabCount: currentTabId ? 1 : 0 });
      }
      break;

    case 'cdpToggle':
      if (active) {
        await fullDisconnect();
        sendResponse({ active: false });
      } else {
        try {
          await cdpEnable();
          sendResponse({ active: true, error: null });
        } catch (err) {
          sendResponse({ active: false, error: err.message });
        }
      }
      break;

    case 'cdpForceCleanup':
      await fullDisconnect();
      sendResponse({ done: true });
      break;
  }
}

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

  if (!currentTabId) {
    setCurrentTabId(tab.id);
  }

  stopReconnect();

  await attachDebugger(currentTabId);

  const wsUrl = base.replace(/^http/, 'ws') + '/api/browser/ws?token=' + encodeURIComponent(mcpToken);

  // 保存连接信息和 MCP token 用于自动重连
  reconnectServerBase = base;
  reconnectUrl = wsUrl;
  reconnectAttempt = 0;
  savedMcpToken = mcpToken;
  saveReconnectState();

  await ensureOffscreenDocument();

  let wsHandler;
  const openPromise = new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      if (wsHandler) chrome.runtime.onMessage.removeListener(wsHandler);
      reject(new Error('连接超时(5s)'));
    }, CONFIG.connectionTimeout);

    wsHandler = function handler(msg) {
      if (msg.type === 'offscreen-open') {
        clearTimeout(timeout);
        if (wsHandler) chrome.runtime.onMessage.removeListener(wsHandler);
        resolve();
      } else if (msg.type === 'offscreen-error') {
        clearTimeout(timeout);
        if (wsHandler) chrome.runtime.onMessage.removeListener(wsHandler);
        reject(new Error(msg.error || 'WebSocket 连接失败'));
      }
    };
    chrome.runtime.onMessage.addListener(wsHandler);
  });
  try {
    wsReplacing = true;
    wsGraceUntil = Infinity;
    sendToOffscreen('offscreen-connect', { url: wsUrl });
    await openPromise;
  } finally {
    wsReplacing = false;
    wsGraceUntil = 0;
    if (wsHandler) {
      chrome.runtime.onMessage.removeListener(wsHandler);
      wsHandler = null;
    }
  }

  active = true;
  updateBadgeConnected();

  await cleanupStaleGroups();
  addTabToGroup(currentTabId).catch(() => {});
}

// 用户主动断开 — 完整清理，不重连
async function fullDisconnect() {
  stopReconnect();
  active = false;
  reconnectServerBase = null; // 清内存状态，阻止 scheduleReconnect 启动
  reconnectUrl = null;
  savedMcpToken = '';
  await detachDebugger(currentTabId);
  sendToOffscreen('offscreen-disconnect', {});
  await ungroupAll();
  updateBadgeOff();
}

// ─── Service Worker 存活保持 ────────────────────────────────────────────────────

chrome.runtime.onConnect.addListener((port) => {
  if (port.name === 'sw-keepalive') {
    port.onDisconnect.addListener(() => {});
  }
});

// ─── Service Worker 启动时恢复状态 ──────────────────────────────────────────────

let restorePromiseDone = false;
let restoreResolve;
const restorePromise = new Promise(r => { restoreResolve = r; });

async function restoreState() {
  try {
    const contexts = await chrome.runtime.getContexts({
      contextTypes: ['OFFSCREEN_DOCUMENT'],
      documentUrls: [chrome.runtime.getURL('offscreen.html')],
    });
    if (contexts.length === 0) return;

    const resp = await chrome.runtime.sendMessage({ type: 'offscreen-status' });
    if (resp && resp.connected) {
      active = true;
      connectionState = 'connected';
      updateBadgeConnected();
      if (currentTabId) {
        addTabToGroup(currentTabId).catch(() => {});
      }
    }
  } catch {}
}

(async () => {
  await restoreState();
  if (!active) cleanupStaleGroups().catch(() => {});
  restorePromiseDone = true;
  restoreResolve();
})();
