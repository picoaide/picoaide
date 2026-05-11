export async function init(ctx) {
  const { Api, esc, showMsg } = ctx;
  let page = 1;
  let pageSize = 50;
  let totalPages = 1;
  let search = '';

  // 获取认证模式
  const usersData = await Api.get('/api/admin/users?runtime=false&page=1&page_size=1').catch(() => ({}));
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
  ctx.$('#wl-search-btn')?.addEventListener('click', () => {
    search = ctx.$('#wl-search-input').value.trim();
    page = 1;
    loadWhitelist();
  });
  ctx.$('#wl-search-input')?.addEventListener('keydown', e => {
    if (e.key === 'Enter') {
      search = e.target.value.trim();
      page = 1;
      loadWhitelist();
    }
  });
  ctx.$('#wl-page-size')?.addEventListener('change', e => {
    pageSize = Number(e.target.value) || 50;
    page = 1;
    loadWhitelist();
  });
  ctx.$('#wl-prev')?.addEventListener('click', () => {
    if (page > 1) {
      page--;
      loadWhitelist();
    }
  });
  ctx.$('#wl-next')?.addEventListener('click', () => {
    if (page < totalPages) {
      page++;
      loadWhitelist();
    }
  });

  async function loadWhitelist() {
    const tags = ctx.$('#wl-tags');
    tags.innerHTML = '';
    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
    if (search) params.set('search', search);
    const data = await Api.get('/api/admin/whitelist?' + params.toString());
    if (!data.success) return;
    page = data.page || page;
    pageSize = data.page_size || pageSize;
    totalPages = data.total_pages || 1;
    updatePager(data);

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

  function updatePager(data) {
    const info = ctx.$('#wl-page-info');
    if (info) info.textContent = '第 ' + (data.page || 1) + ' / ' + (data.total_pages || 1) + ' 页，共 ' + (data.total || 0) + ' 个用户';
    const sizeSel = ctx.$('#wl-page-size');
    if (sizeSel) sizeSel.value = String(pageSize);
    const prev = ctx.$('#wl-prev');
    const next = ctx.$('#wl-next');
    if (prev) prev.disabled = page <= 1;
    if (next) next.disabled = page >= totalPages;
  }

  async function removeUser(username) {
    if (!confirm('移除 ' + username + '？')) return;
    const res = await Api.post('/api/admin/whitelist', { remove: username });
    showMsg('#wl-msg', res.message || res.error, res.success);
    if (res.success) loadWhitelist();
  }

  async function addUsers() {
    const input = ctx.$('#wl-add-input');
    const newUsers = input.value.split(',').map(s => s.trim()).filter(Boolean);
    if (newUsers.length === 0) return;

    const res = await Api.post('/api/admin/whitelist', { add: newUsers.join(',') });
    showMsg('#wl-msg', res.message || res.error, res.success);
    if (res.success) { input.value = ''; loadWhitelist(); }
  }
}
