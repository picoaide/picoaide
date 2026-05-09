export async function init(ctx) {
  const { Api, esc, showMsg } = ctx;

  // 获取认证模式
  const usersData = await Api.get('/api/admin/users').catch(() => ({}));
  const isUnifiedAuth = usersData.unified_auth || false;

  if (!isUnifiedAuth) {
    // 本地模式：隐藏白名单编辑，显示提示
    const wlSection = ctx.$('#wl-section');
    if (wlSection) {
      wlSection.innerHTML = '<div class="msg msg-info">本地认证模式下无需白名单管理，用户由管理员手动创建。</div>';
    }
    return;
  }

  await loadWhitelist();
  ctx.$('#wl-add-btn')?.addEventListener('click', addUsers);

  async function loadWhitelist() {
    const tags = ctx.$('#wl-tags');
    tags.innerHTML = '';
    const data = await Api.get('/api/admin/whitelist');
    if (!data.success) return;

    const users = data.users || [];
    if (users.length === 0) { tags.innerHTML = '<small>白名单为空（所有用户均可使用）</small>'; return; }

    for (const u of users) {
      const tag = document.createElement('span');
      tag.className = 'tag';
      tag.innerHTML = esc(u) + ' <button data-remove="' + esc(u) + '">&times;</button>';
      tags.appendChild(tag);
    }

    tags.querySelectorAll('[data-remove]').forEach(btn => {
      btn.addEventListener('click', () => removeUser(btn.dataset.remove));
    });
  }

  async function removeUser(username) {
    if (!confirm('移除 ' + username + '？')) return;
    const data = await Api.get('/api/admin/whitelist');
    const users = (data.users || []).filter(u => u !== username);
    const res = await Api.post('/api/admin/whitelist', { users: users.join(',') });
    showMsg('#wl-msg', res.message || res.error, res.success);
    if (res.success) loadWhitelist();
  }

  async function addUsers() {
    const input = ctx.$('#wl-add-input');
    const newUsers = input.value.split(',').map(s => s.trim()).filter(Boolean);
    if (newUsers.length === 0) return;

    const data = await Api.get('/api/admin/whitelist');
    const existing = data.users || [];
    const merged = [...new Set([...existing, ...newUsers])];

    const res = await Api.post('/api/admin/whitelist', { users: merged.join(',') });
    showMsg('#wl-msg', res.message || res.error, res.success);
    if (res.success) { input.value = ''; loadWhitelist(); }
  }
}
