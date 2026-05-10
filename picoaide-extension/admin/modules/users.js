export async function init(ctx) {
  const { Api, esc, showMsg } = ctx;

  let isUnifiedAuth = false;
  let groupMap = {};

  ctx.$('#refresh-users').addEventListener('click', () => { loadGroups().then(loadUsers); });
  ctx.$('#new-user-btn')?.addEventListener('click', openCreateModal);

  await loadGroups().then(loadUsers);

  async function loadGroups() {
    groupMap = {};
    const data = await Api.get('/api/admin/groups');
    if (!data.success) return;
    for (const g of (data.groups || [])) {
      const members = await Api.get('/api/admin/groups/members?name=' + encodeURIComponent(g.name));
      if (members.success) {
        for (const u of (members.members || [])) {
          if (!groupMap[u]) groupMap[u] = [];
          groupMap[u].push(g.name);
        }
      }
    }
  }

  async function loadUsers() {
    const tbody = ctx.$('#users-tbody');
    tbody.innerHTML = '<tr><td colspan="6" class="text-center">加载中...</td></tr>';
    ctx.$('#users-empty').classList.add('hidden');

    const data = await Api.get('/api/admin/users');
    if (!data.success) { tbody.innerHTML = ''; return; }

    isUnifiedAuth = data.unified_auth || false;

    const newBtn = ctx.$('#new-user-btn');
    const tip = ctx.$('#unified-auth-tip');
    if (isUnifiedAuth) {
      newBtn?.classList.add('hidden');
      tip?.classList.remove('hidden');
    } else {
      newBtn?.classList.remove('hidden');
      tip?.classList.add('hidden');
    }

    const users = data.users || [];
    if (users.length === 0) { tbody.innerHTML = ''; ctx.$('#users-empty').classList.remove('hidden'); return; }

    tbody.innerHTML = '';
    for (const u of users) {
      if (u.role === 'superadmin') continue;
      const statusCls = u.status === 'running' ? 'badge-ok' : 'badge-muted';
      const imgBadge = u.image_ready
        ? '<span class="badge badge-ok">就绪</span>'
        : '<span class="badge badge-danger">未拉取</span>';
      const noImg = !u.image_ready ? ' disabled title="镜像未拉取"' : '';
      const groups = groupMap[u.username] || [];
      const groupTags = groups.map(g => '<span class="tag">' + esc(g) + '</span>').join(' ');
      const tr = document.createElement('tr');
      let actions = '<div class="btn-group">';
      actions += '<button class="btn btn-sm btn-danger" data-action="delete" data-user="' + esc(u.username) + '">删除</button>';
      actions += '<button class="btn btn-sm btn-outline"' + noImg + ' data-action="start" data-user="' + esc(u.username) + '">启动</button>';
      actions += '<button class="btn btn-sm btn-outline"' + noImg + ' data-action="restart" data-user="' + esc(u.username) + '">重启</button>';
      actions += '<button class="btn btn-sm btn-outline"' + noImg + ' data-action="debug" data-user="' + esc(u.username) + '">调试重启</button>';
      actions += '<button class="btn btn-sm btn-outline" data-action="stop" data-user="' + esc(u.username) + '">停止</button>';
      actions += '<button class="btn btn-sm btn-outline" data-action="apply" data-user="' + esc(u.username) + '">下发配置</button>';
      actions += '<button class="btn btn-sm btn-outline" data-action="logs" data-user="' + esc(u.username) + '">日志</button>';
      actions += '</div>';
      tr.innerHTML = '<td><strong>' + esc(u.username) + '</strong></td><td>' + (groupTags || '<small class="text-muted">-</small>') + '</td><td><span class="badge ' + statusCls + '">' + esc(u.status) + '</span></td><td>' + esc(u.image_tag) + ' ' + imgBadge + '</td><td>' + esc(u.ip || '-') + '</td><td>' + actions + '</td>';
      tbody.appendChild(tr);
    }

    tbody.querySelectorAll('[data-action]').forEach(btn => {
      btn.addEventListener('click', async () => {
        if (btn.dataset.action === 'delete') {
          if (!confirm('确定要删除用户 ' + btn.dataset.user + ' 吗？该操作将归档用户数据并删除账户。')) return;
          const res = await Api.post('/api/admin/users/delete', { username: btn.dataset.user });
          showMsg('#users-msg', res.message || res.error, res.success);
          if (res.success) loadUsers();
          return;
        }
        if (btn.dataset.action === 'logs') {
          openLogModal(btn.dataset.user);
          return;
        }
        if (btn.dataset.action === 'apply') {
          const res = await Api.post('/api/admin/config/apply', { username: btn.dataset.user });
          showMsg('#users-msg', res.message || res.error, res.success);
          return;
        }
        if (btn.dataset.action === 'debug') {
          if (!confirm('确定要以 Picoclaw debug 模式重启用户 ' + btn.dataset.user + ' 的容器吗？日志会更详细。')) return;
        }
        const res = await Api.post('/api/admin/container/' + btn.dataset.action, { username: btn.dataset.user });
        showMsg('#users-msg', res.message || res.error, res.success);
        if (res.success) setTimeout(loadUsers, 1500);
      });
    });
  }

  async function openLogModal(username) {
    ctx.$('#log-modal')?.remove();
    const overlay = document.createElement('div');
    overlay.id = 'log-modal';
    overlay.className = 'modal-overlay';
    overlay.innerHTML =
      '<div class="modal" style="max-width:800px">' +
        '<div class="modal-header">容器日志: ' + esc(username) + '<button id="modal-close">&times;</button></div>' +
        '<div class="modal-body">' +
          '<div class="row mb-1">' +
            '<label style="white-space:nowrap">行数</label>' +
            '<select id="log-tail" style="width:auto">' +
              '<option value="50">50</option>' +
              '<option value="100" selected>100</option>' +
              '<option value="200">200</option>' +
              '<option value="500">500</option>' +
              '<option value="1000">1000</option>' +
            '</select>' +
            '<button class="btn btn-sm btn-primary" id="log-refresh">刷新</button>' +
          '</div>' +
          '<pre id="log-content" style="max-height:500px;overflow:auto;background:#1e1e1e;color:#d4d4d4;padding:12px;font-size:12px;border-radius:4px;white-space:pre-wrap;word-break:break-all;margin:0">加载中...</pre>' +
        '</div>' +
      '</div>';
    ctx.$('#content-area').appendChild(overlay);
    overlay.querySelector('#modal-close').addEventListener('click', () => overlay.remove());
    overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });

    async function fetchLogs() {
      const tail = overlay.querySelector('#log-tail').value;
      const pre = overlay.querySelector('#log-content');
      pre.textContent = '加载中...';
      try {
        const data = await Api.get('/api/admin/container/logs?username=' + encodeURIComponent(username) + '&tail=' + tail);
        if (data.success) {
          pre.textContent = data.logs || '（无日志）';
          pre.scrollTop = pre.scrollHeight;
        } else {
          pre.textContent = data.error || '获取日志失败';
        }
      } catch (e) {
        pre.textContent = e.message;
      }
    }

    overlay.querySelector('#log-refresh').addEventListener('click', fetchLogs);
    fetchLogs();
  }

  function openCreateModal() {
    ctx.$('#create-modal')?.remove();
    const overlay = document.createElement('div');
    overlay.id = 'create-modal';
    overlay.className = 'modal-overlay';
    overlay.innerHTML =
      '<div class="modal">' +
        '<div class="modal-header">新建用户<button id="modal-close">&times;</button></div>' +
        '<div class="modal-body">' +
          '<div class="field">' +
            '<label>用户名（每行一个，支持批量创建）</label>' +
            '<textarea id="batch-usernames" rows="6" placeholder="zhangsan\nlisi\nwangwu" style="min-height:100px"></textarea>' +
          '</div>' +
          '<div class="field">' +
            '<label>镜像标签</label>' +
            '<select id="image-tag-select" style="width:100%;padding:6px 8px;border-radius:4px;border:1px solid #ccc">' +
              '<option value="">加载中...</option>' +
            '</select>' +
          '</div>' +
          '<div id="modal-msg" class="msg"></div>' +
          '<div id="batch-result" class="hidden">' +
            '<div class="msg msg-ok">创建完成，请复制账号密码并告知用户</div>' +
            '<table class="mt-1">' +
              '<thead><tr><th>用户名</th><th>密码</th></tr></thead>' +
              '<tbody id="batch-result-tbody"></tbody>' +
            '</table>' +
          '</div>' +
        '</div>' +
        '<div class="modal-footer">' +
          '<button class="btn btn-outline" id="batch-copy-btn" style="display:none">复制全部</button>' +
          '<button class="btn btn-primary" id="batch-create-btn">创建</button>' +
        '</div>' +
      '</div>';

    ctx.$('#content-area').appendChild(overlay);
    loadLocalTags();

    async function loadLocalTags() {
      const select = overlay.querySelector('#image-tag-select');
      try {
        const data = await Api.get('/api/admin/images/local-tags');
        const tags = data.tags || [];
        select.innerHTML = '';
        if (tags.length === 0) {
          select.innerHTML = '<option value="">无本地镜像，请先拉取</option>';
          return;
        }
        for (const tag of tags) {
          const opt = document.createElement('option');
          opt.value = tag;
          opt.textContent = tag;
          select.appendChild(opt);
        }
      } catch {
        select.innerHTML = '<option value="">加载失败</option>';
      }
    }

    overlay.querySelector('#modal-close').addEventListener('click', () => overlay.remove());
    overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });
    overlay.querySelector('#batch-create-btn').addEventListener('click', batchCreate);
    overlay.querySelector('#batch-copy-btn').addEventListener('click', batchCopy);
  }

  async function batchCreate() {
    const textarea = ctx.$('#batch-usernames');
    const names = textarea.value.split('\n').map(s => s.trim()).filter(Boolean);
    if (names.length === 0) {
      showMsg('#modal-msg', '请输入至少一个用户名', false);
      return;
    }

    const imageTag = ctx.$('#image-tag-select')?.value || '';
    if (!imageTag) {
      showMsg('#modal-msg', '请先拉取镜像', false);
      return;
    }

    showMsg('#modal-msg', '创建中...', true);

    const results = [];
    const errors = [];

    for (const name of names) {
      const res = await Api.post('/api/admin/users/create', { username: name, image_tag: imageTag });
      if (res.success) {
        results.push({ username: res.username, password: res.password });
      } else {
        errors.push(name + ': ' + (res.error || '创建失败'));
      }
    }

    if (errors.length > 0) {
      showMsg('#modal-msg', errors.join('\n'), false);
    } else {
      ctx.$('#modal-msg').textContent = '';
      ctx.$('#modal-msg').className = 'msg';
    }

    if (results.length > 0) {
      const tbody = ctx.$('#batch-result-tbody');
      tbody.innerHTML = '';
      for (const r of results) {
        const tr = document.createElement('tr');
        tr.innerHTML = '<td>' + esc(r.username) + '</td><td style="font-family:monospace">' + esc(r.password) + '</td>';
        tbody.appendChild(tr);
      }
      ctx.$('#batch-result').classList.remove('hidden');
      ctx.$('#batch-copy-btn').style.display = '';
      textarea.value = '';
      ctx.$('#batch-result').dataset.results = JSON.stringify(results);
    }

    await loadUsers();
  }

  async function batchCopy() {
    const resultEl = ctx.$('#batch-result');
    if (!resultEl?.dataset.results) return;
    const results = JSON.parse(resultEl.dataset.results);
    const text = results.map(r => r.username + '\t' + r.password).join('\n');
    try {
      await navigator.clipboard.writeText(text);
      showMsg('#modal-msg', '已复制 ' + results.length + ' 个账号密码', true);
    } catch {
      const ta = document.createElement('textarea');
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
      showMsg('#modal-msg', '已复制 ' + results.length + ' 个账号密码', true);
    }
  }
}
