// 任务面板模块 — 左侧任务队列和历史管理

var panelState = { current: null, queue: [], history: [], selectedId: null };
var panelCtx = null;

// ============================================================
// 初始化
// ============================================================

export async function init(ctx) {
  panelCtx = ctx;
  await refreshTasks();
}

// ============================================================
// 刷新任务列表
// ============================================================

export async function refreshTasks() {
  try {
    var resp = await Api.get('/api/user/task/list?limit=50');
    var tasks = resp.tasks || [];
    panelState.current = tasks.find(function(t) { return t.status === 'running' || t.status === 'paused'; }) || null;
    panelState.queue = tasks.filter(function(t) { return t.status === 'pending'; });
    panelState.history = tasks.filter(function(t) {
      return t.status === 'completed' || t.status === 'cancelled' || t.status === 'failed';
    });
    renderPanel();
  } catch (e) {
    console.error('刷新任务列表失败:', e);
  }
}

// ============================================================
// 处理 SSE 事件更新面板状态
// ============================================================

export function handleTaskEvent(evt) {
  if (!evt || !evt.type) return;

  var changed = false;

  if (evt.type === 'task_started') {
    var d = safeParseEventData(evt.data);
    if (d && d.task_id) {
      // 从队列移到当前
      panelState.queue = panelState.queue.filter(function(t) { return t.id !== d.task_id; });
      if (!panelState.current || panelState.current.id !== d.task_id) {
        panelState.current = {
          id: d.task_id,
          title: d.title || '',
          status: 'running',
          iteration_count: 0,
          created_at: d.created_at || new Date().toISOString()
        };
      } else {
        panelState.current.status = 'running';
      }
      changed = true;
    }
  } else if (evt.type === 'task_completed') {
    var d2 = safeParseEventData(evt.data);
    if (d2 && d2.task_id) {
      var completedTask = panelState.current && panelState.current.id === d2.task_id ? panelState.current : null;
      if (completedTask) {
        completedTask.status = 'completed';
        panelState.history.unshift(completedTask);
        panelState.current = null;
      }
      changed = true;
    }
  } else if (evt.type === 'task_cancelled') {
    var d3 = safeParseEventData(evt.data);
    if (d3 && d3.task_id) {
      var cancelledTask = panelState.current && panelState.current.id === d3.task_id ? panelState.current : null;
      if (cancelledTask) {
        cancelledTask.status = 'cancelled';
        panelState.history.unshift(cancelledTask);
        panelState.current = null;
      }
      changed = true;
    }
  } else if (evt.type === 'task_paused') {
    var d4 = safeParseEventData(evt.data);
    if (d4 && d4.task_id && panelState.current && panelState.current.id === d4.task_id) {
      panelState.current.status = 'paused';
      changed = true;
    }
  } else if (evt.type === 'task_resumed') {
    var d5 = safeParseEventData(evt.data);
    if (d5 && d5.task_id && panelState.current && panelState.current.id === d5.task_id) {
      panelState.current.status = 'running';
      changed = true;
    }
  } else if (evt.type === 'task_failed') {
    var d6 = safeParseEventData(evt.data);
    if (d6 && d6.task_id) {
      var failedTask = panelState.current && panelState.current.id === d6.task_id ? panelState.current : null;
      if (failedTask) {
        failedTask.status = 'failed';
        panelState.history.unshift(failedTask);
        panelState.current = null;
      }
      changed = true;
    }
  } else if (evt.type === 'task_queued') {
    var d7 = safeParseEventData(evt.data);
    if (d7 && d7.task_id) {
      // 可能从 refresh 中已经有了最新状态，简单重新拉取
      changed = true;
    }
  } else if (evt.type === 'iteration') {
    var d8 = safeParseEventData(evt.data);
    if (d8 && panelState.current && panelState.current.id === d8.task_id) {
      panelState.current.iteration_count = d8.count || (panelState.current.iteration_count || 0) + 1;
      changed = true;
    }
  }

  if (changed) {
    renderPanel();
  }
}

function safeParseEventData(data) {
  if (typeof data === 'object') return data;
  try { return JSON.parse(data); } catch(e) { return null; }
}

// ============================================================
// 渲染面板
// ============================================================

function renderPanel() {
  var panel = document.getElementById('task-panel');
  if (!panel) return;

  // 限制历史显示数量
  var historyToShow = panelState.history.slice(0, 20);

  panel.innerHTML =
    renderSection('当前任务', 'current-task', panelState.current ? [panelState.current] : [], true) +
    renderSection('队列', 'task-queue', panelState.queue, false) +
    renderSection('历史', 'task-history', historyToShow, false);

  // 绑定点击事件
  panel.querySelectorAll('.task-item[data-task-id]').forEach(function(el) {
    el.addEventListener('click', function() {
      var taskId = this.getAttribute('data-task-id');
      selectTask(taskId);
    });
  });
}

function renderSection(title, sectionId, tasks, isCurrent) {
  var html = '<div class="task-panel-section">';
  html += '<div class="task-panel-title">' + esc(title) + '</div>';
  html += '<div id="' + sectionId + '">';
  if (tasks.length === 0) {
    html += '<div class="task-item" style="color:var(--text-secondary);font-style:italic">暂无</div>';
  } else {
    tasks.forEach(function(t) {
      var cls = 'task-item';
      if (panelState.selectedId === t.id) cls += ' active';
      html += '<div class="' + cls + '" data-task-id="' + esc(t.id) + '">';
      html += '<span class="task-status ' + esc(t.status) + '"></span>';
      html += '<span class="task-title">' + esc(t.title || t.id || '未命名任务') + '</span>';
      html += '<div class="task-meta">' + formatTaskStatus(t.status) + ' · ' + (t.iteration_count || 0) + '步</div>';
      html += '</div>';
    });
  }
  html += '</div></div>';
  return html;
}

function formatTaskStatus(s) {
  var map = {
    pending: '等待中',
    running: '执行中',
    paused: '已暂停',
    completed: '已完成',
    cancelled: '已取消',
    failed: '失败'
  };
  return map[s] || s;
}

// ============================================================
// 选择任务（加载历史对话）
// ============================================================

export function selectTask(taskId) {
  panelState.selectedId = taskId;
  renderPanel();
  loadTaskHistory(taskId);
}

async function loadTaskHistory(taskId) {
  var box = document.getElementById('chat-box');
  if (!box) return;

  // 显示加载状态
  box.innerHTML = '<div id="chat-loading" class="chat-skeleton">' +
    '<div class="skeleton-line" style="width:75%"></div>' +
    '<div class="skeleton-line" style="width:50%"></div>' +
    '<div class="skeleton-line" style="width:60%"></div>' +
    '</div>';

  try {
    var resp = await Api.get('/api/user/task/events?task_id=' + encodeURIComponent(taskId) + '&limit=200');
    if (resp && resp.events && resp.events.length > 0) {
      box.innerHTML = '';
      // 重置 chat 模块的 SSE 状态
      if (window._chatResetSSEState) {
        window._chatResetSSEState(box);
      }
      // 标记为历史加载，跳过 daemon 状态更新
      window._chatLoadingHistory = true;
      // 逐个回放历史事件
      for (var i = 0; i < resp.events.length; i++) {
        if (window._chatHandleSSEEvent) {
          window._chatHandleSSEEvent(resp.events[i]);
        }
      }
      window._chatLoadingHistory = false;
    } else {
      box.innerHTML = '<div style="text-align:center;padding:32px;color:var(--text-secondary)">该任务暂无对话记录</div>';
    }
  } catch (e) {
    box.innerHTML = '<div style="text-align:center;padding:32px;color:var(--text-secondary)">加载失败: ' + esc(e.message) + '</div>';
  }
}

// ============================================================
// 暴露全局接口（供 HTML onclick 调用）
// ============================================================

window.taskpanel = {
  init: init,
  refreshTasks: refreshTasks,
  handleTaskEvent: handleTaskEvent,
  selectTask: selectTask
};
