var editingId = 0;

export function init({ Api, esc, showMsg, confirmModal }) {
  var tbody = document.getElementById('cron-tbody');
  var createBtn = document.getElementById('cron-create-btn');
  var modal = document.getElementById('cron-modal');
  var modalTitle = document.getElementById('cron-modal-title');
  var modalClose = document.getElementById('cron-modal-close');
  var modalCancel = document.getElementById('cron-modal-cancel');
  var modalOk = document.getElementById('cron-modal-ok');
  var promptInput = document.getElementById('cron-prompt-input');
  var scheduleInput = document.getElementById('cron-schedule-input');

  if (!tbody) return;

  function showModal() { modal.style.display = 'flex'; }
  function hideModal() { modal.style.display = 'none'; editingId = 0; promptInput.value = ''; scheduleInput.value = ''; }

  modalClose.addEventListener('click', hideModal);
  modalCancel.addEventListener('click', hideModal);
  modal.addEventListener('click', function(e) { if (e.target === modal) hideModal(); });

  async function loadList() {
    try {
      var resp = await Api.get('/api/cron');
      if (!resp.success) { showMsg('cron-msg', '加载失败', false); return; }
      renderJobs(resp.jobs || []);
    } catch (e) {
      showMsg('cron-msg', e.message, false);
    }
  }

  function renderJobs(jobs) {
    if (!jobs.length) {
      tbody.innerHTML = '<tr><td colspan="7" class="text-muted" style="text-align:center">暂无定时任务，点击"新建任务"创建</td></tr>';
      return;
    }
    tbody.innerHTML = jobs.map(function(j) {
      var desc = describeSchedule(j.schedule);
      var nextRun = j.next_run_at ? new Date(j.next_run_at).toLocaleString('zh-CN') : '-';
      var lastRun = j.last_run_at ? new Date(j.last_run_at).toLocaleString('zh-CN') : '-';
      var statusHtml = j.enabled
        ? '<span style="color:var(--green)">启用</span>'
        : '<span style="color:var(--red)">禁用</span>';
      return '<tr>' +
        '<td>' + esc(j.id) + '</td>' +
        '<td>' + esc(j.prompt) + '</td>' +
        '<td style="font-size:.85rem">' + esc(desc) + '</td>' +
        '<td style="font-size:.85rem">' + esc(nextRun) + '</td>' +
        '<td style="font-size:.85rem">' + esc(lastRun) + '</td>' +
        '<td>' + statusHtml + '</td>' +
        '<td class="action-cell">' +
          '<button class="btn btn-xs btn-outline cron-toggle" data-id="' + j.id + '">' + (j.enabled ? '禁用' : '启用') + '</button> ' +
          '<button class="btn btn-xs btn-outline cron-edit" data-id="' + j.id + '" data-prompt="' + esc(j.prompt) + '" data-schedule="' + esc(j.schedule) + '">编辑</button> ' +
          '<button class="btn btn-xs btn-ghost cron-delete" data-id="' + j.id + '" style="color:var(--red)">删除</button>' +
        '</td>' +
      '</tr>';
    }).join('');

    // 事件绑定
    tbody.querySelectorAll('.cron-toggle').forEach(function(btn) {
      btn.addEventListener('click', function() { toggleJob(parseInt(this.dataset.id)); });
    });
    tbody.querySelectorAll('.cron-edit').forEach(function(btn) {
      btn.addEventListener('click', function() {
        editingId = parseInt(this.dataset.id);
        promptInput.value = this.dataset.prompt;
        scheduleInput.value = this.dataset.schedule;
        modalTitle.textContent = '编辑定时任务';
        showModal();
      });
    });
    tbody.querySelectorAll('.cron-delete').forEach(function(btn) {
      btn.addEventListener('click', function() { deleteJob(parseInt(this.dataset.id)); });
    });
  }

  function describeSchedule(schedule) {
    var parts = (schedule || '').split(' ');
    if (parts[0] === 'cron') return 'cron ' + parts.slice(1).join(' ');
    if (parts[0] === 'every') {
      var ms = parseInt(parts[1]);
      if (ms >= 86400000) return '每 ' + (ms / 86400000) + ' 天';
      if (ms >= 3600000) return '每 ' + (ms / 3600000) + ' 小时';
      if (ms >= 60000) return '每 ' + (ms / 60000) + ' 分钟';
      return '每 ' + ms + ' 毫秒';
    }
    if (parts[0] === 'at') return '一次性';
    return schedule || '-';
  }

  async function toggleJob(id) {
    try {
      var resp = await Api.post('/api/cron/toggle', { id: id });
      showMsg('cron-msg', resp.success ? '已切换' : resp.error, resp.success);
      if (resp.success) loadList();
    } catch (e) {
      showMsg('cron-msg', e.message, false);
    }
  }

  async function deleteJob(id) {
    var ok = await confirmModal('确定要删除定时任务 #' + id + ' 吗？');
    if (!ok) return;
    try {
      var resp = await Api.post('/api/cron/delete', { id: id });
      showMsg('cron-msg', resp.success ? '已删除' : resp.error, resp.success);
      if (resp.success) loadList();
    } catch (e) {
      showMsg('cron-msg', e.message, false);
    }
  }

  function validateSchedule(s) {
    if (!s) return '调度表达式不能为空';
    var parts = s.split(' ');
    if (parts[0] === 'cron') {
      if (parts.length !== 6) return 'cron 表达式需要 5 个字段（分 时 日 月 周）';
    } else if (parts[0] === 'every') {
      if (isNaN(parseInt(parts[1]))) return 'every 后需要毫秒数';
    } else if (parts[0] === 'at') {
      if (isNaN(parseInt(parts[1]))) return 'at 后需要 unix 毫秒时间戳';
    } else {
      return '不支持的调度类型（支持 cron/every/at）';
    }
    return '';
  }

  createBtn.addEventListener('click', function() {
    editingId = 0;
    promptInput.value = '';
    scheduleInput.value = '';
    modalTitle.textContent = '新建定时任务';
    showModal();
  });

  modalOk.addEventListener('click', async function() {
    var prompt = promptInput.value.trim();
    var schedule = scheduleInput.value.trim();
    var err = validateSchedule(schedule);
    if (err) { showMsg('cron-msg', err, false); return; }
    if (!prompt) { showMsg('cron-msg', '提示词不能为空', false); return; }

    try {
      var resp;
      if (editingId) {
        resp = await Api.post('/api/cron/update', { id: editingId, schedule: schedule, prompt: prompt });
      } else {
        resp = await Api.post('/api/cron/create', { schedule: schedule, prompt: prompt });
      }
      showMsg('cron-msg', resp.success ? '保存成功' : resp.error, resp.success);
      if (resp.success) { hideModal(); loadList(); }
    } catch (e) {
      showMsg('cron-msg', e.message, false);
    }
  });

  loadList();
}
