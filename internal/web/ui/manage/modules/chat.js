// AI 对话模块（SSE 流式输出，支持断线重连）

var currentReader = null;
var stopRequested = false;
var currentRunId = null;
var RUN_ID_KEY = 'picoaide_chat_run_id';
var isUserScrolledUp = false;
var lastSentText = '';
var timeoutTimer = null;
var sendGeneration = 0;

function resetTimeoutTimer() {
  if (timeoutTimer) { clearTimeout(timeoutTimer); timeoutTimer = null; }
  var el = document.getElementById('chat-timeout-hint');
  if (el) el.style.display = 'none';
}

function startTimeoutTimer() {
  resetTimeoutTimer();
  timeoutTimer = setTimeout(function() {
    var el = document.getElementById('chat-timeout-hint');
    if (el) {
      el.textContent = '⏳ 响应时间较长，请稍候...';
      el.style.display = '';
    }
  }, 30000);
}

function scrollToBottom(box) {
  if (!box) return;
  box.scrollTop = box.scrollHeight;
  isUserScrolledUp = false;
  var btn = document.getElementById('chat-scroll-bottom');
  if (btn) btn.style.display = 'none';
}

function scrollToBottomIfNeeded(box) {
  if (!isUserScrolledUp) {
    scrollToBottom(box);
  }
}

function updateScrollButton(box) {
  var btn = document.getElementById('chat-scroll-bottom');
  if (!btn || !box) return;
  var threshold = box.scrollHeight - box.clientHeight - 120;
  if (box.scrollTop < threshold) {
    isUserScrolledUp = true;
    btn.style.display = '';
  } else {
    isUserScrolledUp = false;
    btn.style.display = 'none';
  }
}

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

// 给代码块添加语言标签和复制按钮
function enhanceCodeBlocks(root) {
  if (!root) return;
  root.querySelectorAll('pre').forEach(function(pre) {
    if (pre.querySelector('.code-header')) return;
    var code = pre.querySelector('code');
    if (!code) return;
    var lang = '';
    if (code.className) {
      var m = code.className.match(/language-(\w+)/);
      if (m) lang = m[1];
    }
    var header = document.createElement('div');
    header.className = 'code-header';
    header.style.cssText = 'display:flex;justify-content:space-between;align-items:center;padding:4px 10px;background:#e8e8e8;border-radius:6px 6px 0 0;font-size:12px;color:#666';
    var langLabel = document.createElement('span');
    langLabel.textContent = lang || 'code';
    var copyBtn = document.createElement('button');
    copyBtn.textContent = '复制';
    copyBtn.style.cssText = 'border:none;background:transparent;cursor:pointer;color:#4a9eff;font-size:12px;padding:2px 6px;border-radius:3px';
    copyBtn.addEventListener('click', function(e) {
      e.stopPropagation();
      var text = code.textContent;
      navigator.clipboard.writeText(text).then(function() {
        copyBtn.textContent = '✅ 已复制';
        setTimeout(function() { copyBtn.textContent = '复制'; }, 2000);
      }).catch(function() {
        copyBtn.textContent = '复制失败';
      });
    });
    header.appendChild(langLabel);
    header.appendChild(copyBtn);
    pre.insertBefore(header, pre.firstChild);
    pre.style.cssText += ';border-radius:6px;overflow:hidden;margin:8px 0';
  });
}

// 空状态引导卡片
var examplePrompts = [
  '帮我写一封商务邮件',
  '搜索项目中的配置文件',
  '解释这段代码的作用'
];

function showEmptyPrompt(box) {
  if (!box) return;
  box.innerHTML =
    '<div style="text-align:center;padding:32px 20px;color:var(--text-secondary)">' +
    '<div style="font-size:18px;margin-bottom:6px">💬 开始与 AI 助手对话</div>' +
    '<div style="font-size:13px;margin-bottom:20px">试试这些问题：</div>' +
    examplePrompts.map(function(p) {
      return '<div class="example-prompt" style="display:inline-block;margin:4px 6px;padding:8px 16px;border:1px solid var(--border-light,#e0e0e0);border-radius:8px;cursor:pointer;font-size:14px;background:var(--card-bg,#fff);transition:background .15s" onmouseover="this.style.background=\'#f5f5f5\'" onmouseout="this.style.background=\'var(--card-bg,#fff)\'">' + escapeHtml(p) + '</div>';
    }).join('') +
    '</div>';
  // 点击示例问题自动发送
  box.querySelectorAll('.example-prompt').forEach(function(el) {
    el.addEventListener('click', function() {
      var text = this.textContent;
      var input = document.getElementById('chat-input');
      if (input) input.value = text;
      var form = document.getElementById('chat-form');
      if (form) form.dispatchEvent(new Event('submit'));
    });
  });
}

// 格式化时间戳
function formatTime(ts) {
  var d = ts ? new Date(ts) : new Date();
  var now = new Date();
  var isToday = d.toDateString() === now.toDateString();
  var yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  var isYesterday = d.toDateString() === yesterday.toDateString();
  var h = d.getHours().toString().padStart(2, '0');
  var m = d.getMinutes().toString().padStart(2, '0');
  var t = h + ':' + m;
  if (isToday) return t;
  if (isYesterday) return '昨天 ' + t;
  return (d.getMonth() + 1) + '月' + d.getDate() + '日 ' + t;
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
  var ts = formatTime();
  box.insertAdjacentHTML('beforeend',
    '<div class="chat-msg ' + cls + '"' +
    (role === 'assistant' ? ' id="last-ai-msg"' : '') +
    ' data-ts="' + Date.now() + '">' +
    '<div class="chat-meta">' + label + '</div>' +
    '<div class="chat-content">' + renderMarkdown(content) + '</div>' +
    '<div class="chat-ts" style="font-size:11px;color:#bbb;margin-top:4px">' + ts + '</div>' +
    '</div>');
  var lastMsg = box.querySelector('.chat-msg:last-child');
  var lastContent = lastMsg && lastMsg.querySelector('.chat-content');
  if (lastContent) enhanceCodeBlocks(lastContent);
  // 助手消息添加复制按钮
  if (role === 'assistant' && lastMsg) {
    var copyBtn = document.createElement('button');
    copyBtn.textContent = '📋';
    copyBtn.title = '复制';
    copyBtn.style.cssText = 'position:absolute;top:6px;right:6px;border:none;background:rgba(200,200,200,.5);cursor:pointer;font-size:14px;padding:2px 6px;border-radius:4px;opacity:0;transition:opacity .2s';
    copyBtn.addEventListener('click', function(e) {
      e.stopPropagation();
      navigator.clipboard.writeText(content).then(function() {
        copyBtn.textContent = '✅';
        setTimeout(function() { copyBtn.textContent = '📋'; }, 2000);
      });
    });
    lastMsg.style.position = 'relative';
    lastMsg.appendChild(copyBtn);
    lastMsg.addEventListener('mouseenter', function() { copyBtn.style.opacity = '1'; });
    lastMsg.addEventListener('mouseleave', function() { copyBtn.style.opacity = '0'; });
  }
  scrollToBottom(box);
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

var sseState = { box: null, contentEl: null, fullText: '', statusEl: null, thinkingEl: null, reasoningEl: null };

function handleSSEEvent(parsed) {
  var s = sseState;
  if (parsed.type === 'user_message') {
    var txt = safeParse(parsed.data);
    if (typeof txt !== 'string') txt = String(txt);
    appendMsg(s.box, 'user', txt);
  } else if (parsed.type === 'reasoning') {
    var rt = safeParse(parsed.data);
    if (typeof rt !== 'string') rt = String(rt);
    if (!s.reasoningEl) {
      s.box.insertAdjacentHTML('beforeend',
        '<div class="chat-msg msg-reasoning" style="background:#f8f9fa;border:1px solid #e8e8e8;border-radius:8px;margin-bottom:8px;max-width:85%">' +
        '<div class="reasoning-header" style="display:flex;justify-content:space-between;align-items:center;padding:8px 14px;cursor:pointer;user-select:none" onclick="this.nextElementSibling.classList.toggle(\'reasoning-collapsed\');this.querySelector(\'.reasoning-toggle\').textContent=this.nextElementSibling.classList.contains(\'reasoning-collapsed\')?\'\u25b6\':\'\u25bc\'">' +
        '<span class="chat-meta" style="margin:0">🤔 思考中...</span>' +
        '<span class="reasoning-toggle" style="font-size:12px;color:#999">▼</span>' +
        '</div>' +
        '<div class="chat-reasoning-content" style="font-size:.88em;color:#666;line-height:1.5;white-space:pre-wrap;word-break:break-word;padding:0 14px 10px">' +
        '</div>' +
        '</div>');
      s.reasoningEl = s.box.querySelector('.chat-msg.msg-reasoning:last-child .chat-reasoning-content');
    }
    resetTimeoutTimer();
    s.reasoningEl.textContent += rt;
    scrollToBottomIfNeeded(s.box);
  } else if (parsed.type === 'text_delta') {
    var txt = safeParse(parsed.data);
    if (typeof txt !== 'string') txt = String(txt);
    s.fullText += txt;
    // 有 reasoning 时，第一条 text_delta 代表思考结束，隐藏 reasoning 块
    if (s.reasoningEl) {
      var reasoningMsg = s.reasoningEl.closest('.chat-msg.msg-reasoning');
      if (reasoningMsg) {
        var meta = reasoningMsg.querySelector('.chat-meta');
        if (meta) meta.textContent = '🤔 思考完成';
      }
      s.reasoningEl = null;
    }
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
    enhanceCodeBlocks(s.contentEl);
    scrollToBottomIfNeeded(s.box);
  } else if (parsed.type === 'tool_call_start') {
    resetTimeoutTimer();
    var td = safeParse(parsed.data);
    var toolName = td.name || '?';
    var inputStr = '';
    if (td.input) {
      try { inputStr = JSON.stringify(safeParse(td.input), null, 2); }
      catch(e) { inputStr = String(td.input); }
    }
    var toolId = td.id || 'tool-' + Date.now();
    var toolCardId = 'tool-card-' + toolId;
    // 移除旧的临时状态，用卡片替换
    if (s.statusEl && s.statusEl.id === 'chat-tool-status') {
      s.statusEl.remove();
      s.statusEl = null;
    }
    s.box.insertAdjacentHTML('beforeend',
      '<div class="tool-card" id="' + toolCardId + '" style="margin-bottom:8px;border:1px solid #e0e0e0;border-radius:8px;overflow:hidden;max-width:85%">' +
      '<div class="tool-card-header" style="display:flex;justify-content:space-between;align-items:center;padding:6px 12px;background:#f5f5f5;cursor:pointer;user-select:none" onclick="var b=document.getElementById(\'' + toolCardId + '\').querySelector(\'.tool-card-body\');b.classList.toggle(\'reasoning-collapsed\');this.querySelector(\'.tool-toggle\').textContent=b.classList.contains(\'reasoning-collapsed\')?\'\u25b6\':\'\u25bc\'">' +
      '<span>🔧 <strong>' + escapeHtml(toolName) + '</strong></span>' +
      '<span class="tool-toggle" style="font-size:12px;color:#999">▼</span>' +
      '</div>' +
      '<div class="tool-card-body" style="padding:8px 12px;font-size:13px;background:#fafafa"><pre style="margin:0;white-space:pre-wrap;word-break:break-word;font-size:12px;background:none;padding:0">' + escapeHtml(inputStr.length > 500 ? inputStr.substring(0, 500) + '...' : inputStr) + '</pre></div>' +
      '</div>');
    scrollToBottomIfNeeded(s.box);
  } else if (parsed.type === 'tool_result') {
    resetTimeoutTimer();
    var tr = safeParse(parsed.data);
    var toolName = tr.name || '';
    var resultStr = '';
    if (tr.result) {
      try { resultStr = JSON.stringify(safeParse(tr.result), null, 2); }
      catch(e) { resultStr = String(tr.result); }
    }
    // 找到对应 tool card 更新状态
    var toolId = tr.id;
    var toolCard = toolId ? document.getElementById('tool-card-' + toolId) : null;
    if (toolCard) {
      var body = toolCard.querySelector('.tool-card-body');
      if (body) {
        body.innerHTML = '<div style="color:#27ae60;margin-bottom:4px">✅ 完成</div><pre style="margin:0;white-space:pre-wrap;word-break:break-word;font-size:12px;background:none;padding:0;max-height:200px;overflow-y:auto">' + escapeHtml(resultStr.length > 1000 ? resultStr.substring(0, 1000) + '...' : resultStr) + '</pre>';
      }
    }
    scrollToBottomIfNeeded(s.box);
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
    resetTimeoutTimer();
    if (s.statusEl && s.statusEl.id === 'chat-tool-status') { s.statusEl.remove(); s.statusEl = null; }
    if (s.reasoningEl) {
      var reasoningMsg = s.reasoningEl.closest('.chat-msg.msg-reasoning');
      if (reasoningMsg) {
        var meta = reasoningMsg.querySelector('.chat-meta');
        if (meta) meta.textContent = '🤔 思考完成';
      }
      s.reasoningEl = null;
    }
    // AI 回复已完成，断开 SSE 流，不等待沙箱退出
    if (currentReader) {
      try { currentReader.cancel(); } catch(e) {}
    }
    scrollToBottomIfNeeded(s.box);
    return; // 提前返回，让 readSSEStream 的 reader.read() catch 到错误后退出
  } else if (parsed.type === 'reasoning_complete') {
    // 不再使用（由 finish 事件处理），静默忽略
  } else if (parsed.type === 'error') {
    resetTimeoutTimer();
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
      s.contentEl.innerHTML = '<div style="color:red">错误: ' + escapeHtml(errMsg) + '</div>';
    }
    // 添加重试按钮
    var lastMsg = s.box.querySelector('.chat-msg:last-child');
    if (lastMsg && lastSentText) {
      var retryBtn = document.createElement('button');
      retryBtn.textContent = '🔄 重试';
      retryBtn.style.cssText = 'margin-top:6px;padding:4px 12px;border:1px solid #4a9eff;border-radius:4px;background:#fff;color:#4a9eff;font-size:13px;cursor:pointer';
      retryBtn.addEventListener('click', function() {
        retryBtn.textContent = '⏳ 发送中...';
        retryBtn.disabled = true;
        sendMessage(lastSentText);
      });
      lastMsg.after(retryBtn);
    }
    scrollToBottomIfNeeded(s.box);
  } else if (parsed.type === 'progress_startup') {
    var sd = safeParse(parsed.data);
    if (typeof sd !== 'object') sd = {};
    var startupEl = document.getElementById('chat-startup-progress');
    if (!startupEl) {
      startupEl = document.createElement('div');
      startupEl.id = 'chat-startup-progress';
      startupEl.style.cssText = 'padding:8px 14px;margin:4px 0 8px;background:#f0f7ff;border:1px solid #d0e4f7;border-radius:8px;font-size:13px;color:#4a9eff';
      // 插在用户消息和 AI 回复之间
      var lastUser = s.box.querySelector('.chat-msg.msg-user:last-child');
      if (lastUser && lastUser.nextSibling) {
        s.box.insertBefore(startupEl, lastUser.nextSibling);
      } else {
        s.box.appendChild(startupEl);
      }
    }
    var stageNames = {mounting_overlay: '准备容器', picoagent_started: '启动 AI 引擎'};
    startupEl.innerHTML = '🚀 ' + (stageNames[sd.stage] || sd.stage) + '...';
  } else if (parsed.type === 'compressing') {
    var cp = safeParse(parsed.data);
    var el = document.getElementById('chat-compress-hint');
    if (cp && cp.status === 'start') {
      if (!el) {
        el = document.createElement('div');
        el.id = 'chat-compress-hint';
        el.style.cssText = 'padding:6px 14px;font-size:12px;color:#999;text-align:center';
        s.box.appendChild(el);
      }
      el.textContent = '📦 正在压缩对话历史...';
    } else if (el) {
      el.textContent = '';
      el.style.display = 'none';
    }
  } else if (parsed.type === 'llm_retry') {
    var rd = safeParse(parsed.data);
    var retryEl = document.getElementById('chat-llm-retry');
    if (!retryEl) {
      retryEl = document.createElement('div');
      retryEl.id = 'chat-llm-retry';
      retryEl.style.cssText = 'padding:4px 14px;font-size:12px;color:#e67e22;text-align:center';
      s.box.appendChild(retryEl);
    }
    if (rd && rd.retry < rd.max) {
      retryEl.textContent = '⚠️ LLM 请求超时，正在重试 (' + rd.retry + '/' + rd.max + ')...';
    } else {
      retryEl.textContent = '';
      retryEl.style.display = 'none';
    }
  } else if (parsed.type === 'tool_progress') {
    var tp = safeParse(parsed.data);
    if (tp && tp.status === 'running') {
      if (!s.statusEl) {
        s.statusEl = document.createElement('div');
        s.statusEl.id = 'chat-tool-status';
        s.statusEl.style.cssText = 'padding:4px 14px 0;font-size:13px;color:var(--text-secondary,#999)';
        s.box.appendChild(s.statusEl);
      }
      s.statusEl.innerHTML = '🔧 正在执行 <strong>' + escapeHtml(String(tp.tool)) + '</strong>...';
    }
    scrollToBottomIfNeeded(s.box);
  } else if (parsed.type === 'subagent_event') {
    var sa = safeParse(parsed.data);
    if (sa && sa.sub_type) {
      var saBox = document.getElementById('chat-subagent-' + escapeHtml(String(sa.name)));
      if (!saBox) {
        s.box.insertAdjacentHTML('beforeend',
          '<div class="tool-card" id="chat-subagent-' + escapeHtml(String(sa.name)) + '" style="margin-bottom:8px;border:1px solid #e0e0e0;border-radius:8px;overflow:hidden;max-width:85%">' +
          '<div class="tool-card-header" style="padding:6px 12px;background:#f0f0ff;font-size:13px">🔀 子代理「' + escapeHtml(String(sa.name)) + '」</div>' +
          '<div class="tool-card-body" style="padding:8px 12px;font-size:12px;color:#666;max-height:150px;overflow-y:auto"></div>' +
          '</div>');
        saBox = document.getElementById('chat-subagent-' + escapeHtml(String(sa.name)));
      }
      var body = saBox && saBox.querySelector('.tool-card-body');
      if (body) {
        var subData = sa.data || '';
        var text = typeof subData === 'string' ? subData : JSON.stringify(subData);
        body.innerHTML = escapeHtml(text.substring(0, 200));
      }
    }
    scrollToBottomIfNeeded(s.box);
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
  sseState.reasoningEl = null;
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
  var gen = ++sendGeneration;
  lastSentText = text;
  // 取消前一次未关闭的 reader（快速点击发送时）
  if (currentReader) {
    try { currentReader.cancel(); } catch(e) {}
    currentReader = null;
  }
  var box = document.getElementById('chat-box');
  var sendBtn = document.getElementById('chat-send-btn');
  var stopBtn = document.getElementById('chat-stop-btn');
  var input = document.getElementById('chat-input');
  stopRequested = false;

  // 清除之前遗留的重生成按钮
  box.querySelectorAll('.regen-btn').forEach(function(b) { b.remove(); });

  if (box.children.length === 1 && box.querySelector('div[style*="text-align:center"]')) {
    box.innerHTML = '';
  }

  // 消息和助手回复均由 SSE 事件渲染
  input.value = '';
  sendBtn.style.display = 'none';
  stopBtn.style.display = 'inline-block';
  stopBtn.disabled = false;
  // 发送状态反馈
  var originalSendText = sendBtn.textContent;
  sendBtn.textContent = '⏳ 发送中...';

  resetSSEState(box);
  scrollToBottom(box);
  startTimeoutTimer();

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
      if (sseState.contentEl) {
        sseState.contentEl.innerHTML = '<div style="color:red">错误: ' + escapeHtml(sendData.error || '提交消息失败') + '</div>';
      } else if (box) {
        box.insertAdjacentHTML('beforeend',
          '<div class="chat-msg msg-assistant"><div class="chat-meta">助手</div><div class="chat-content" style="color:red">错误: ' + escapeHtml(sendData.error || '提交消息失败') + '</div></div>');
      }
      return;
    }
    var runId = sendData.run_id;
    localStorage.setItem(RUN_ID_KEY, runId);

    await readSSEStream(runId);
  } catch (e) {
    if (!stopRequested) {
      var errText = '请求失败: ' + escapeHtml(e.message);
      if (sseState.contentEl) {
        sseState.contentEl.innerHTML = '<div style="color:red">' + errText + '</div>';
      } else if (box) {
        box.insertAdjacentHTML('beforeend',
          '<div class="chat-msg msg-assistant"><div class="chat-meta">助手</div><div class="chat-content" style="color:red">' + errText + '</div></div>');
      }
    }
  } finally {
    currentReader = null;
    currentRunId = null;
    localStorage.removeItem(RUN_ID_KEY);
    // 只有最新的 sendMessage 才能改按钮状态
    if (gen === sendGeneration) {
      sendBtn.style.display = 'inline-block';
      stopBtn.style.display = 'none';
      stopBtn.disabled = true;
      sendBtn.textContent = originalSendText;
    }
    var dots = box.querySelector('.chat-thinking-dot');
    if (dots) dots.remove();
  }

  // 流结束后在助手消息底部添加重新生成按钮（仅最新一代）
  if (gen === sendGeneration && !stopRequested && lastSentText) {
    var lastAssistant = box.querySelector('.chat-msg.msg-assistant:last-child');
    if (lastAssistant && !lastAssistant.querySelector('.regen-btn')) {
      var regenBtn = document.createElement('button');
      regenBtn.className = 'regen-btn';
      regenBtn.textContent = '🔄 重新生成';
      regenBtn.style.cssText = 'margin-top:6px;padding:4px 12px;border:1px solid #e0e0e0;border-radius:4px;background:#fafafa;color:#666;font-size:12px;cursor:pointer';
      regenBtn.addEventListener('click', function() {
        sendMessage(lastSentText);
      });
      lastAssistant.appendChild(regenBtn);
    }
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
    // 忽略浮动元素（timeout-hint、scroll-bottom），检查是否有真实消息
    var realChildren = 0;
    for (var i = 0; i < box.children.length; i++) {
      var el = box.children[i];
      if (el.id !== 'chat-timeout-hint' && el.id !== 'chat-scroll-bottom' && !el.classList.contains('chat-skeleton')) {
        realChildren++;
      }
    }
    if (realChildren > 0) return;
    if (res.events && res.events.length > 0) {
      // 优先用 events 还原完整对话状态（含工具调用等）
      box.innerHTML = '';
      resetSSEState(box);
      for (var evt of res.events) {
        handleSSEEvent(evt);
      }
    } else if (res.messages && res.messages.length > 0) {
      box.innerHTML = '';
      for (var m of res.messages) {
        appendMsg(box, m.role === 'assistant' ? 'assistant' : 'user', m.content);
      }
    } else {
      showEmptyPrompt(box);
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

  var gen = ++sendGeneration;

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
  // 只有最新一代才能改按钮
  if (gen !== sendGeneration) return false;
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

async function checkActiveRun() {
  try {
    var res = await apiJSON('GET', '/api/user/chat/active');
    if (res.active && res.run_id) {
      localStorage.setItem(RUN_ID_KEY, res.run_id);
      return res.run_id;
    }
  } catch(e) {}
  return null;
}

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

  // 先检查服务器是否有活跃会话（跨刷新/SPA导航）
  checkActiveRun().then(function(serverRunId) {
    var localRunId = localStorage.getItem(RUN_ID_KEY);
    var runId = serverRunId || localRunId;
    if (runId) {
      localStorage.setItem(RUN_ID_KEY, runId);
      tryReconnect(true);
    } else {
      loadChatHistory();
    }
  });

  // 滚动锁定：监听用户手动滚动
  var box = document.getElementById('chat-box');
  if (box) {
    box.addEventListener('scroll', function() {
      updateScrollButton(box);
    });
    var scrollBtn = document.getElementById('chat-scroll-bottom');
    if (scrollBtn) {
      scrollBtn.addEventListener('click', function() {
        scrollToBottom(box);
      });
    }
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
    // Shift+Enter 换行，Enter 发送
    input.addEventListener('keydown', function(e) {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        form.dispatchEvent(new Event('submit'));
      }
    });
    // 自动伸缩高度
    function autoResize() {
      input.style.height = 'auto';
      input.style.height = Math.min(input.scrollHeight, 200) + 'px';
    }
    input.addEventListener('input', autoResize);
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
