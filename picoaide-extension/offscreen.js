// offscreen.js — 持久化 WebSocket 连接管理
// 在 offscreen document 中运行，不受 Service Worker 30 秒生命周期限制

let ws = null;
let keepaliveTimer = null;

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
    chrome.runtime.sendMessage({ type: 'offscreen-open' });
    startKeepalive();
  };

  ws.onmessage = (event) => {
    chrome.runtime.sendMessage({ type: 'offscreen-message', data: event.data });
  };

  ws.onclose = (event) => {
    stopKeepalive();
    chrome.runtime.sendMessage({ type: 'offscreen-close', code: event.code });
    ws = null;
  };

  ws.onerror = () => {
    // onclose will fire after onerror
  };
}

function disconnectWebSocket() {
  stopKeepalive();
  if (ws) {
    try { ws.close(); } catch {}
    ws = null;
  }
}

function startKeepalive() {
  stopKeepalive();
  keepaliveTimer = setInterval(() => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      try { ws.send(''); } catch {}
    }
  }, 20000);
}

function stopKeepalive() {
  if (keepaliveTimer) {
    clearInterval(keepaliveTimer);
    keepaliveTimer = null;
  }
}
