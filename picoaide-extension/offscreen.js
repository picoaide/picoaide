// offscreen.js — 持久化 WebSocket 连接管理
// 在 offscreen document 中运行，不受 Service Worker 30 秒生命周期限制

let ws = null;
let keepaliveTimer = null;
let swPort = null; // 长连接端口，保持 Service Worker 存活

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (msg.type === 'offscreen-connect') {
    connectWebSocket(msg.url);
    sendResponse({ ok: true });
    return false;
  }
  if (msg.type === 'offscreen-disconnect') {
    disconnectWebSocket();
    sendResponse({ ok: true });
    return false;
  }
  if (msg.type === 'offscreen-send') {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(msg.data);
    }
    return false;
  }
  if (msg.type === 'offscreen-status') {
    sendResponse({
      connected: ws !== null && ws.readyState === WebSocket.OPEN,
    });
    return false;
  }
});

// 保持 Service Worker 存活：建立长连接端口
function connectSWKeepalive() {
  try {
    swPort = chrome.runtime.connect({ name: 'sw-keepalive' });
    swPort.onDisconnect.addListener(() => {
      swPort = null;
      // Service Worker 被杀，尝试重连（会唤醒 SW）
      if (ws && ws.readyState === WebSocket.OPEN) {
        setTimeout(connectSWKeepalive, 1000);
      }
    });
  } catch {
    swPort = null;
    if (ws && ws.readyState === WebSocket.OPEN) {
      setTimeout(connectSWKeepalive, 2000);
    }
  }
}

function disconnectSWKeepalive() {
  if (swPort) {
    try { swPort.disconnect(); } catch {}
    swPort = null;
  }
}

function connectWebSocket(url) {
  if (ws) {
    try { ws.close(); } catch {}
    ws = null;
  }

  try {
    ws = new WebSocket(url);
  } catch (e) {
    chrome.runtime.sendMessage({ type: 'offscreen-error', error: e.message });
    return;
  }

  ws.onopen = () => {
    connectSWKeepalive();
    chrome.runtime.sendMessage({ type: 'offscreen-open' });
    startKeepalive();
  };

  ws.onmessage = (event) => {
    chrome.runtime.sendMessage({ type: 'offscreen-message', data: event.data });
  };

  ws.onclose = (event) => {
    disconnectSWKeepalive();
    stopKeepalive();
    chrome.runtime.sendMessage({ type: 'offscreen-close', code: event.code });
    ws = null;
  };

  ws.onerror = () => {
    // onclose will fire after onerror
  };
}

function disconnectWebSocket() {
  disconnectSWKeepalive();
  stopKeepalive();
  if (ws) {
    try { ws.close(); } catch {}
    ws = null;
  }
}

const KEEPALIVE_INTERVAL = 20000;

function startKeepalive() {
  stopKeepalive();
  keepaliveTimer = setInterval(() => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      try { ws.send(''); } catch {}
    }
  }, KEEPALIVE_INTERVAL);
}

function stopKeepalive() {
  if (keepaliveTimer) {
    clearInterval(keepaliveTimer);
    keepaliveTimer = null;
  }
}
