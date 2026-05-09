export async function init(ctx) {
  const { Api, esc, showMsg } = ctx;

  ctx.$('#refresh-superadmins').addEventListener('click', loadAdmins);
  ctx.$('#create-superadmin-btn').addEventListener('click', openCreateModal);

  await loadAdmins();

  async function loadAdmins() {
    const tbody = ctx.$('#sa-tbody');
    tbody.innerHTML = '<tr><td colspan="2" class="text-center">加载中...</td></tr>';
    ctx.$('#sa-empty').classList.add('hidden');

    const data = await Api.get('/api/admin/superadmins');
    if (!data.success) { tbody.innerHTML = ''; return; }

    const admins = data.admins || [];
    if (admins.length === 0) { tbody.innerHTML = ''; ctx.$('#sa-empty').classList.remove('hidden'); return; }

    tbody.innerHTML = '';
    for (const name of admins) {
      const tr = document.createElement('tr');
      tr.innerHTML = '<td><strong>' + esc(name) + '</strong></td><td><div class="btn-group">' +
        '<button class="btn btn-sm btn-outline" data-reset="' + esc(name) + '">重置密码</button>' +
        '<button class="btn btn-sm btn-danger" data-del="' + esc(name) + '">删除</button>' +
        '</div></td>';
      tbody.appendChild(tr);
    }

    tbody.querySelectorAll('[data-reset]').forEach(btn => {
      btn.addEventListener('click', async () => {
        if (!confirm('确定重置 ' + btn.dataset.reset + ' 的密码？')) return;
        showMsg('#sa-msg', '重置中...', true);
        const res = await Api.post('/api/admin/superadmins/reset', { username: btn.dataset.reset });
        if (res.success) {
          showMsg('#sa-msg', res.message + '，新密码: ' + res.password, true);
        } else {
          showMsg('#sa-msg', res.error, false);
        }
      });
    });

    tbody.querySelectorAll('[data-del]').forEach(btn => {
      btn.addEventListener('click', async () => {
        if (!confirm('确定删除超管 ' + btn.dataset.del + '？')) return;
        const res = await Api.post('/api/admin/superadmins/delete', { username: btn.dataset.del });
        showMsg('#sa-msg', res.message || res.error, res.success);
        if (res.success) loadAdmins();
      });
    });
  }

  function openCreateModal() {
    ctx.$('#sa-modal')?.remove();
    const overlay = document.createElement('div');
    overlay.id = 'sa-modal';
    overlay.className = 'modal-overlay';
    overlay.innerHTML =
      '<div class="modal">' +
        '<div class="modal-header">新建超管<button id="modal-close">&times;</button></div>' +
        '<div class="modal-body">' +
          '<div class="field"><label>用户名</label><input type="text" id="sa-name" placeholder="admin2"></div>' +
          '<div id="sa-modal-msg" class="msg"></div>' +
        '</div>' +
        '<div class="modal-footer"><button class="btn btn-primary" id="sa-create-btn">创建</button></div>' +
      '</div>';
    ctx.$('#content-area').appendChild(overlay);
    overlay.querySelector('#modal-close').addEventListener('click', () => overlay.remove());
    overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });
    overlay.querySelector('#sa-create-btn').addEventListener('click', async () => {
      const name = ctx.$('#sa-name').value.trim();
      if (!name) { alert('请输入用户名'); return; }
      showMsg('#sa-modal-msg', '创建中...', true);
      const res = await Api.post('/api/admin/superadmins/create', { username: name });
      if (res.success) {
        overlay.remove();
        showMsg('#sa-msg', '超管 ' + res.username + ' 创建成功，密码: ' + res.password, true);
        loadAdmins();
      } else {
        showMsg('#sa-modal-msg', res.error, false);
      }
    });
  }
}
