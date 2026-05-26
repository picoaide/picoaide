// AI 对话模块（SSE 流式输出，支持断线重连）

var currentReader = null;
var stopRequested = false;
var currentRunId = null;
var RUN_ID_KEY = 'picoaide_chat_run_id';

// ============================================================
// Markdown 渲染 — 使用 marked（带表格、GFM 等完整支持）
// ============================================================
var markedPromise = null;
function ensureMarked() {
  if (window.marked) return Promise.resolve(window.marked);
  if (markedPromise) return markedPromise;
  markedPromise = new Promise(function(resolve, reject) {
    var s = document.createElement('script');
    s.src = '/js/marked.min.js';
    s.onload = function() { resolve(window.marked); };
    s.onerror = function() { reject(new Error('marked.js 加载失败')); };
    document.head.appendChild(s);
  });
  return markedPromise;
}

function renderMarkdown(text) {
  // 先用 marked 渲染（如果已加载），否则回退到简单转义
  if (window.marked) {
    try {
      return window.marked.parse(text, { breaks: true, gfm: true });
    } catch(e) { /* fall through */ }
  }
  return '<p>' + escapeHtml(text).replace(/\n/g, '<br>') + '</p>';
}

function escapeHtml(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

// safeParse 安全解析事件数据（SSE 中 Data 可能是序列化字符串或已解析对象）
function safeParse(data) {
  if (typeof data === 'object') return data;
  try { return JSON.parse(data); } catch(e) { return data; }
}

// ============================================================
// 消息追加
// ============================================================

function appendMsg(box, role, content) {
  var cls = role === 'user' ? 'msg-user' : 'msg-assistant';
  var label = role === 'user' ? '你' : '助手';
  box.insertAdjacentHTML('beforeend',
    '<div class="chat-msg ' + cls + '"' +
    (role === 'assistant' ? ' id="last-ai-msg"' : '') +
    '>' +
    '<div class="chat-meta">' + label + '</div>' +
    '<div class="chat-content">' + renderMarkdown(content) + '</div>' +
    '</div>');
  box.scrollTop = box.scrollHeight;
}

function getLastAssistantContent(box) {
  var msgs = box.querySelectorAll('.chat-msg.msg-assistant');
  if (msgs.length > 0) {
    return msgs[msgs.length - 1].querySelector('.chat-content');
  }
  return null;
}

// ============================================================
// SSE 事件处理 — 渲染到聊天框
// ============================================================

var sseState = { box: null, contentEl: null, fullText: '', statusEl: null, thinkingEl: null };

function handleSSEEvent(parsed) {
  var s = sseState;
  if (parsed.type === 'user_message') {
    var txt = safeParse(parsed.data);
    if (typeof txt !== 'string') txt = String(txt);
    appendMsg(s.box, 'user', txt);
  } else if (parsed.type === 'text_delta') {
    var txt = safeParse(parsed.data);
    if (typeof txt !== 'string') txt = String(txt);
    s.fullText += txt;
    if (!s.contentEl) {
      if (s.thinkingEl) { s.thinkingEl.remove(); s.thinkingEl = null; }
      s.box.insertAdjacentHTML('beforeend',
        '<div class="chat-msg msg-assistant">' +
        '<div class="chat-meta">助手 <span class="chat-thinking-dot"></span></div>' +
        '<div class="chat-content"></div>' +
        '</div>');
      s.contentEl = s.box.querySelector('.chat-msg.msg-assistant:last-child .chat-content');
      s.thinkingEl = s.box.querySelector('.chat-msg.msg-assistant:last-child .chat-thinking-dot');
    }
    if (s.thinkingEl) { s.thinkingEl.remove(); s.thinkingEl = null; }
    s.contentEl.innerHTML = renderMarkdown(s.fullText);
    s.box.scrollTop = s.box.scrollHeight;
  } else if (parsed.type === 'tool_call_start') {
    var td = safeParse(parsed.data);
    var toolName = td.name || '?';
    var inputPreview = '';
    if (td.input) {
      try { var inp = safeParse(td.input); inputPreview = JSON.stringify(inp).substring(0, 80); }
      catch(e) { inputPreview = String(td.input).substring(0, 80); }
    }
    if (!s.statusEl) {
      s.statusEl = document.createElement('div');
      s.statusEl.id = 'chat-tool-status';
      s.statusEl.style.cssText = 'padding:4px 14px 0;font-size:13px;color:var(--text-secondary,#999)';
      var lastMsg = s.box.querySelector('.chat-msg.msg-assistant:last-child');
      if (lastMsg) lastMsg.after(s.statusEl); else s.box.appendChild(s.statusEl);
    }
    s.statusEl.innerHTML = '🔧 使用 <strong>' + escapeHtml(toolName) + '</strong>(' + escapeHtml(inputPreview) + ')';
    s.box.scrollTop = s.box.scrollHeight;
  } else if (parsed.type === 'tool_result') {
    var tr = safeParse(parsed.data);
    var toolName = tr.name || '';
    var resultPreview = '';
    if (tr.result) { resultPreview = String(tr.result).substring(0, 60); }
    if (!s.statusEl) {
      s.statusEl = document.createElement('div');
      s.statusEl.id = 'chat-tool-status';
      s.statusEl.style.cssText = 'padding:4px 14px 0;font-size:13px;color:var(--text-secondary,#999)';
      var lastMsg = s.box.querySelector('.chat-msg.msg-assistant:last-child');
      if (lastMsg) lastMsg.after(s.statusEl); else s.box.appendChild(s.statusEl);
    }
    s.statusEl.innerHTML = resultPreview
      ? '✅ ' + escapeHtml(toolName) + ' → ' + escapeHtml(resultPreview)
      : '✅ ' + escapeHtml(toolName) + ' 完成';
    s.box.scrollTop = s.box.scrollHeight;
  } else if (parsed.type === 'progress') {
    var pd = safeParse(parsed.data);
    if (typeof pd !== 'object') pd = {};
    if (pd.context_window > 0) {
      var pct = Math.min(100, Math.round(pd.token_count / pd.context_window * 100));
      var bar = document.getElementById('chat-context-bar');
      var fill = document.getElementById('chat-context-fill');
      var label = document.getElementById('chat-context-label');
      if (bar) {
        bar.style.display = '';
        fill.style.width = pct + '%';
        if (pct > 80) fill.style.background = '#e74c3c';
        else if (pct > 60) fill.style.background = '#f39c12';
        else fill.style.background = '#4a9eff';
      }
      if (label) {
        label.textContent = pct + '% (' + pd.token_count + '/' + pd.context_window + ')';
      }
    }
  } else if (parsed.type === 'finish') {
    if (s.statusEl) { s.statusEl.remove(); s.statusEl = null; }
    s.box.scrollTop = s.box.scrollHeight;
  } else if (parsed.type === 'error') {
    var errMsg = safeParse(parsed.data);
    if (typeof errMsg !== 'string') errMsg = String(errMsg);
    if (!s.contentEl) {
      if (s.thinkingEl) { s.thinkingEl.remove(); s.thinkingEl = null; }
      s.box.insertAdjacentHTML('beforeend',
        '<div class="chat-msg msg-assistant">' +
        '<div class="chat-meta">助手</div>' +
        '<div class="chat-content">错误: ' + escapeHtml(errMsg) + '</div>' +
        '</div>');
    } else {
      s.contentEl.innerHTML = '错误: ' + errMsg;
    }
    s.box.scrollTop = s.box.scrollHeight;
  }
}

// ============================================================
// SSE 流式读取（通用，首次发送和重连共用）
// ============================================================

function resetSSEState(box) {
  sseState.box = box;
  sseState.contentEl = null;
  sseState.fullText = '';
  sseState.statusEl = null;
  sseState.thinkingEl = null;
  // 重置上下文进度条
  var bar = document.getElementById('chat-context-bar');
  var fill = document.getElementById('chat-context-fill');
  var label = document.getElementById('chat-context-label');
  if (bar) { bar.style.display = 'none'; fill.style.width = '0%'; fill.style.background = '#4a9eff'; }
  if (label) label.textContent = '0%';
}

async function readSSEStream(runId) {
  var box = document.getElementById('chat-box');
  if (!box) return false;
  resetSSEState(box);

  var base = window.location.origin.replace(/\/+$/, '');
  var streamResp = await fetch(base + '/api/user/chat/stream?run_id=' + encodeURIComponent(runId), {
    method: 'GET',
    credentials: 'include',
  });
  if (!streamResp.ok) return false;

  var reader = streamResp.body.getReader();
  currentReader = reader;
  currentRunId = runId;
  var decoder = new TextDecoder();
  var buffer = '';

  while (true) {
    var result;
    try {
      result = await reader.read();
    } catch (e) {
      if (stopRequested) break;
      throw e;
    }
    if (result.done) break;

    buffer += decoder.decode(result.value, { stream: true });
    while (true) {
      var idx = buffer.indexOf('\n');
      if (idx < 0) break;
      var line = buffer.substring(0, idx).trim();
      buffer = buffer.substring(idx + 1);
      if (!line) continue;

      if (line.startsWith('data: ')) {
        var data = line.slice(6);
        try {
          var parsed = JSON.parse(data);
          handleSSEEvent(parsed);
        } catch(e) {
          // 非 JSON 行忽略
        }
      }
    }
  }
  return true;
}

// ============================================================
// 发送消息（SSE 流式）
// ============================================================

async function sendMessage(text) {
  var box = document.getElementById('chat-box');
  var sendBtn = document.getElementById('chat-send-btn');
  var stopBtn = document.getElementById('chat-stop-btn');
  var input = document.getElementById('chat-input');
  stopRequested = false;

  if (box.children.length === 1 && box.querySelector('div[style*="text-align:center"]')) {
    box.innerHTML = '';
  }

  // 消息和助手回复均由 SSE 事件渲染
  input.value = '';
  sendBtn.style.display = 'none';
  stopBtn.style.display = 'inline-block';
  stopBtn.disabled = false;

  resetSSEState(box);
  box.scrollTop = box.scrollHeight;

  try {
    var base = window.location.origin.replace(/\/+$/, '');
    var sendResp = await fetch(base + '/api/user/chat/send', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formBody({ message: text }),
      credentials: 'include',
    });
    var sendData = await sendResp.json();
    if (!sendData.success || !sendData.run_id) {
      contentEl.innerHTML = '错误: ' + (sendData.error || '提交消息失败');
      return;
    }
    var runId = sendData.run_id;
    localStorage.setItem(RUN_ID_KEY, runId);

    await readSSEStream(runId);
  } catch (e) {
    if (!stopRequested && contentEl) contentEl.innerHTML = '请求失败: ' + e.message;
  } finally {
    currentReader = null;
    currentRunId = null;
    localStorage.removeItem(RUN_ID_KEY);
    sendBtn.style.display = 'inline-block';
    stopBtn.style.display = 'none';
    stopBtn.disabled = true;
    var dots = box.querySelector('.chat-thinking-dot');
    if (dots) dots.remove();
  }
}

// ============================================================
// 加载对话历史
// ============================================================

async function loadChatHistory() {
  var box = document.getElementById('chat-box');
  if (!box) return;
  try {
    var res = await apiJSON('GET', '/api/user/chat/history');
    var loading = document.getElementById('chat-loading');
    if (loading) loading.remove();
    if (box.children.length > 0) return;
    if (res.messages && res.messages.length > 0) {
      box.innerHTML = '';
      for (var m of res.messages) {
        appendMsg(box, m.role === 'assistant' ? 'assistant' : 'user', m.content);
      }
    } else {
      box.innerHTML = '<div style="text-align:center;padding:32px;color:var(--text-secondary)">开始你的第一次对话吧！</div>';
    }
  } catch (e) {
    var loading = document.getElementById('chat-loading');
    if (loading) loading.textContent = '加载失败';
  }
}

// ============================================================
// 断线重连 — 用户回来继续接收输出
// ============================================================

async function tryReconnect(force) {
  var runId = localStorage.getItem(RUN_ID_KEY);
  if (!runId) return false;
  if (!force && currentRunId === runId) return true; // 已经在读

  // 取消旧的 reader（SPA 导航保留的旧连接，渲染到已卸载的 DOM）
  if (currentReader) {
    try { currentReader.cancel(); } catch(e) {}
    currentReader = null;
  }
  currentRunId = null;

  var box = document.getElementById('chat-box');
  if (!box) return false;
  box.innerHTML = '';
  resetSSEState(box);

  stopRequested = false;
  var sendBtn = document.getElementById('chat-send-btn');
  var stopBtn = document.getElementById('chat-stop-btn');
  sendBtn.style.display = 'none';
  stopBtn.style.display = 'inline-block';
  stopBtn.disabled = false;

  var ok = await readSSEStream(runId);
  if (!ok) {
    // run 已不存在（已完成超过 5 分钟）
    localStorage.removeItem(RUN_ID_KEY);
    currentRunId = null;
    sendBtn.style.display = 'inline-block';
    stopBtn.style.display = 'none';
    stopBtn.disabled = true;
    loadChatHistory();
    return false;
  }
  // 流结束（正常完成或出错）
  currentRunId = null;
  localStorage.removeItem(RUN_ID_KEY);
  sendBtn.style.display = 'inline-block';
  stopBtn.style.display = 'none';
  stopBtn.disabled = true;
  return true;
}

// ============================================================
// 页面可见性恢复
// ============================================================

var lastVisibleTime = Date.now();
document.addEventListener('visibilitychange', function() {
  if (!document.hidden) {
    if (Date.now() - lastVisibleTime > 3000) {
      tryReconnect();
    }
    lastVisibleTime = Date.now();
  } else {
    lastVisibleTime = Date.now();
  }
});

// ============================================================
// 初始化
// ============================================================
// 预加载 marked 库
ensureMarked();

function init() {
  // 计算面板高度撑满视口剩余空间
  var panel = document.getElementById('chat-panel');
  if (panel) {
    var navbar = document.querySelector('.navbar');
    var container = document.querySelector('.container');
    var header = document.querySelector('.page-header');
    var tabs = document.querySelector('.tabs');
    function adjustHeight() {
      var h = 0;
      if (navbar) h += navbar.offsetHeight;
      if (container) h += parseFloat(getComputedStyle(container).paddingTop) || 0;
      if (header) h += header.offsetHeight + (parseFloat(getComputedStyle(header).marginBottom) || 0);
      if (tabs) h += tabs.offsetHeight + (parseFloat(getComputedStyle(tabs).marginBottom) || 0);
      if (container) h += parseFloat(getComputedStyle(container).paddingBottom) || 0;
      panel.style.height = Math.max(200, window.innerHeight - h) + 'px';
    }
    adjustHeight();
    window.addEventListener('resize', adjustHeight);
  }

  var runId = localStorage.getItem(RUN_ID_KEY);
  if (runId) {
    tryReconnect(true); // force=true 绕过模块缓存的 currentRunId 守卫
  } else {
    loadChatHistory();
  }

  var form = document.getElementById('chat-form');
  var input = document.getElementById('chat-input');
  var stopBtn = document.getElementById('chat-stop-btn');
  if (form && input) {
    form.addEventListener('submit', function(e) {
      e.preventDefault();
      var text = input.value.trim();
      if (text) sendMessage(text);
    });
  }
  if (stopBtn) {
    stopBtn.addEventListener('click', function() {
      stopRequested = true;
      if (currentReader) {
        currentReader.cancel();
      }
      fetch('/api/user/chat/stop', { method: 'POST', credentials: 'include' }).catch(function(){});
      var box = document.getElementById('chat-box');
      var contentEl = getLastAssistantContent(box);
      if (contentEl) {
        contentEl.insertAdjacentHTML('beforeend', '<div style="color:var(--text-secondary,#999);font-size:13px;padding-top:6px">⏹ 已停止</div>');
      }
      localStorage.removeItem(RUN_ID_KEY);
      currentRunId = null;
    });
  }
}

export { init };
