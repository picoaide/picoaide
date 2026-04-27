export async function init(ctx) {
  const { Api, esc, showMsg } = ctx;

  let isUnifiedAuth = false;

  ctx.$('#refresh-users').addEventListener('click', loadUsers);
  ctx.$('#new-user-btn')?.addEventListener('click', openCreateModal);

  await loadUsers();

  async function loadUsers() {
    const tbody = ctx.$('#users-tbody');
    tbody.innerHTML = '<tr><td colspan="5" class="text-center">加载中...</td></tr>';
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
      const statusCls = u.status?.startsWith('Up') ? 'badge-ok' : 'badge-muted';
      const imgBadge = u.image_ready
        ? '<span class="badge badge-ok">就绪</span>'
        : '<span class="badge badge-danger">未拉取</span>';
      const noImg = !u.image_ready ? ' disabled title="镜像未拉取"' : '';
      const tr = document.createElement('tr');
      let actions = '<div class="btn-group">';
      if (!isUnifiedAuth) {
        actions += '<button class="btn btn-sm btn-danger" data-action="delete" data-user="' + esc(u.username) + '">删除</button>';
      }
      actions += '<button class="btn btn-sm btn-outline"' + noImg + ' data-action="start" data-user="' + esc(u.username) + '">启动</button>';
      actions += '<button class="btn btn-sm btn-outline"' + noImg + ' data-action="restart" data-user="' + esc(u.username) + '">重启</button>';
      actions += '<button class="btn btn-sm btn-outline" data-action="stop" data-user="' + esc(u.username) + '">停止</button>';
      actions += '</div>';
      tr.innerHTML = '<td><strong>' + esc(u.username) + '</strong></td><td><span class="badge ' + statusCls + '">' + esc(u.status) + '</span></td><td>' + esc(u.image_tag) + ' ' + imgBadge + '</td><td>' + esc(u.ip || '-') + '</td><td>' + actions + '</td>';
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
        const res = await Api.post('/api/admin/container/' + btn.dataset.action, { username: btn.dataset.user });
        showMsg('#users-msg', res.message || res.error, res.success);
        if (res.success) setTimeout(loadUsers, 1500);
      });
    });
  }

  function openCreateModal() {
    // 如果已有弹窗先移除
    ctx.$('#create-modal')?.remove();

    const overlay = document.createElement('div');
    overlay.id = 'create-modal';
    overlay.className = 'modal-overlay';
    overlay.innerHTML = `
      <div class="modal">
        <div class="modal-header">新建用户<button id="modal-close">&times;</button></div>
        <div class="modal-body">
          <div class="field">
            <label>用户名（每行一个，支持批量创建）</label>
            <textarea id="batch-usernames" rows="6" placeholder="zhangsan&#10;lisi&#10;wangwu" style="min-height:100px"></textarea>
          </div>
          <div id="modal-msg" class="msg"></div>
          <div id="batch-result" class="hidden">
            <div class="msg msg-ok">创建完成，请复制账号密码并告知用户</div>
            <table class="mt-1">
              <thead><tr><th>用户名</th><th>密码</th></tr></thead>
              <tbody id="batch-result-tbody"></tbody>
            </table>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-outline" id="batch-copy-btn" style="display:none">复制全部</button>
          <button class="btn btn-primary" id="batch-create-btn">创建</button>
        </div>
      </div>`;

    ctx.$('#content-area').appendChild(overlay);

    // 关闭
    overlay.querySelector('#modal-close').addEventListener('click', () => overlay.remove());
    overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });

    // 批量创建
    overlay.querySelector('#batch-create-btn').addEventListener('click', batchCreate);
    // 批量复制
    overlay.querySelector('#batch-copy-btn').addEventListener('click', batchCopy);
  }

  async function batchCreate() {
    const textarea = ctx.$('#batch-usernames');
    const names = textarea.value.split('\n').map(s => s.trim()).filter(Boolean);
    if (names.length === 0) {
      showMsg('#modal-msg', '请输入至少一个用户名', false);
      return;
    }

    const msgEl = ctx.$('#modal-msg');
    showMsg('#modal-msg', '创建中...', true);

    const results = [];
    const errors = [];

    for (const name of names) {
      const res = await Api.post('/api/admin/users/create', { username: name });
      if (res.success) {
        results.push({ username: res.username, password: res.password });
      } else {
        errors.push(name + ': ' + (res.error || '创建失败'));
      }
    }

    // 显示错误
    if (errors.length > 0) {
      showMsg('#modal-msg', errors.join('\n'), false);
    } else {
      msgEl.textContent = '';
      msgEl.className = 'msg';
    }

    // 显示结果
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

      // 保存结果供复制
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
      // fallback
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
