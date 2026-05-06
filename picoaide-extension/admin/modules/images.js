export async function init(ctx) {
  const { Api, esc, showMsg, $ } = ctx;

  let localImages = [];

  await loadLocalImages();
  loadRegistryTags();

  $('#images-refresh-registry')?.addEventListener('click', loadRegistryTags);
  $('#pull-modal-close')?.addEventListener('click', closePullModal);
  $('#pull-close-btn')?.addEventListener('click', closePullModal);
  $('#migrate-modal-close')?.addEventListener('click', closeMigrateModal);
  $('#migrate-cancel-btn')?.addEventListener('click', closeMigrateModal);
  $('#image-users-close')?.addEventListener('click', closeUsersModal);
  $('#image-users-close-btn')?.addEventListener('click', closeUsersModal);
  $('#upgrade-modal-close')?.addEventListener('click', closeUpgradeModal);
  $('#upgrade-cancel-btn')?.addEventListener('click', closeUpgradeModal);
  $('#upgrade-select-all')?.addEventListener('click', () => {
    document.querySelectorAll('#upgrade-users input[type=checkbox]').forEach(cb => cb.checked = true);
  });
  $('#upgrade-deselect-all')?.addEventListener('click', () => {
    document.querySelectorAll('#upgrade-users input[type=checkbox]').forEach(cb => cb.checked = false);
  });

  async function loadLocalImages() {
    const data = await Api.get('/api/admin/images').catch(() => ({ images: [] }));
    const tbody = $('#images-local');
    tbody.innerHTML = '';
    localImages = data.images || [];
    if (localImages.length === 0) {
      $('#images-local-empty')?.classList.remove('hidden');
      return;
    }
    $('#images-local-empty')?.classList.add('hidden');
    for (const img of localImages) {
      const tags = (img.repo_tags || []).join(', ') || '(无标签)';
      const userCount = img.user_count || 0;
      const userBtnHtml = userCount > 0
        ? '<button class="btn btn-sm btn-outline" data-users-img="' + esc(tags) + '">' + userCount + ' 个用户</button>'
        : '<small class="text-muted">-</small>';
      const tr = document.createElement('tr');
      const actionsTd = '<td></td>';
      tr.innerHTML =
        '<td style="font-family:monospace;font-size:12px;word-break:break-all">' + esc(tags) + '</td>' +
        '<td style="font-family:monospace;font-size:12px">' + esc(img.id || '-') + '</td>' +
        '<td>' + esc(img.size_str) + '</td>' +
        '<td style="white-space:nowrap">' + esc(img.created_str || '-') + '</td>' +
        '<td>' + userBtnHtml + '</td>' +
        actionsTd;
      for (const tag of (img.repo_tags || [])) {
        const btn = document.createElement('button');
        btn.className = 'btn btn-sm btn-danger';
        btn.textContent = '删除';
        btn.dataset.image = tag;
        btn.addEventListener('click', () => deleteImage(tag));
        tr.querySelector('td:last-child').appendChild(btn);
      }
      tbody.appendChild(tr);
    }
    tbody.querySelectorAll('[data-users-img]').forEach(btn => {
      btn.addEventListener('click', () => showImageUsers(btn.dataset.usersImg));
    });
  }

  async function loadRegistryTags() {
    const tbody = $('#images-registry');
    tbody.innerHTML = '';
    let data;
    try {
      data = await Api.get('/api/admin/images/registry');
    } catch (e) {
      showMsg('#images-registry-msg', '获取远程标签失败: ' + e.message, false);
      $('#images-registry-empty')?.classList.remove('hidden');
      return;
    }
    if (!data.success) {
      showMsg('#images-registry-msg', data.error || '获取远程标签失败', false);
      $('#images-registry-empty')?.classList.remove('hidden');
      return;
    }
    const tags = data.tags || [];
    if (tags.length === 0) {
      $('#images-registry-empty')?.classList.remove('hidden');
      return;
    }
    $('#images-registry-empty')?.classList.add('hidden');

    // 收集本地已有的 tag
    const localTags = new Set();
    for (const img of localImages) {
      for (const t of (img.repo_tags || [])) {
        const idx = t.lastIndexOf(':');
        if (idx > 0) localTags.add(t.substring(idx + 1));
      }
    }

    for (const tag of tags) {
      const exists = localTags.has(tag);
      // 本地存在其他版本的 picoaide 镜像就显示升级按钮
      const hasOtherVersions = Array.from(localTags).some(t => t !== tag);
      const tr = document.createElement('tr');
      const statusHtml = exists
        ? '<span class="badge badge-ok">已存在</span>'
        : '<span class="badge badge-muted">未拉取</span>';
      const upgradeBtnHtml = hasOtherVersions
        ? '<button class="btn btn-sm btn-primary" data-upgrade="' + esc(tag) + '" style="margin-left:4px">升级</button>'
        : '';
      tr.innerHTML =
        '<td style="font-family:monospace">' + esc(tag) + '</td>' +
        '<td>' + statusHtml + '</td>' +
        '<td><button class="btn btn-sm btn-outline" data-tag="' + esc(tag) + '">' + (exists ? '重新拉取' : '拉取') + '</button>' + upgradeBtnHtml + '</td>';
      tbody.appendChild(tr);
    }
    tbody.querySelectorAll('[data-tag]').forEach(btn => {
      btn.addEventListener('click', () => pullImage(btn.dataset.tag));
    });
    tbody.querySelectorAll('[data-upgrade]').forEach(btn => {
      btn.addEventListener('click', () => openUpgradeModal(btn.dataset.upgrade));
    });
  }

  async function pullImage(tag) {
    const modal = $('#image-pull-modal');
    const progress = $('#pull-progress');
    const nameEl = $('#pull-image-name');

    nameEl.textContent = tag;
    progress.innerHTML = '';
    modal.classList.remove('hidden');

    const csrf = await getCSRF();
    const formData = new FormData();
    formData.append('tag', tag);
    formData.append('csrf_token', csrf);

    try {
      const serverBase = await getServerUrl();
      const response = await fetch(serverBase + '/api/admin/images/pull', {
        method: 'POST',
        body: formData,
        credentials: 'include',
      });

      if (!response.ok) {
        progress.innerHTML += '<div style="color:#e74c3c">拉取失败: ' + response.status + '</div>';
        return;
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          if (line.startsWith('data: ')) {
            const data = line.slice(6);
            try {
              const obj = JSON.parse(data);
              if (obj.status === 'done') {
                progress.innerHTML += '<div style="color:#2ecc71">拉取完成!</div>';
                await loadLocalImages();
                loadRegistryTags();
                break;
              }
              if (obj.status) {
                progress.innerHTML += '<div>' + esc(obj.status) + (obj.progress ? ' ' + esc(obj.progress) : '') + '</div>';
              }
            } catch {
              progress.innerHTML += '<div>' + esc(data) + '</div>';
            }
            progress.scrollTop = progress.scrollHeight;
          }
        }
      }
    } catch (err) {
      progress.innerHTML += '<div style="color:#e74c3c">错误: ' + esc(err.message) + '</div>';
    }
  }

  async function getCSRF() {
    const data = await Api.get('/api/csrf');
    return data.csrf_token || '';
  }

  async function deleteImage(imageRef) {
    // 先尝试删除
    const csrf = await getCSRF();
    const serverBase = await getServerUrl();
    try {
      const formData = new FormData();
      formData.append('image', imageRef);
      formData.append('csrf_token', csrf);
      const resp = await fetch(serverBase + '/api/admin/images/delete', {
        method: 'POST',
        body: formData,
        credentials: 'include',
      });
      const data = await resp.json();
      if (data.success) {
        showMsg('#images-local-msg', data.message || '删除成功', true);
        await loadLocalImages();
        loadRegistryTags();
        return;
      }

      // 有用户依赖 — 弹出迁移对话框
      if (resp.status === 409 && data.users && data.users.length > 0) {
        openMigrateModal(imageRef, data.users, data.alternatives || []);
        return;
      }

      let msg = data.error || '删除失败';
      showMsg('#images-local-msg', msg, false);
    } catch (err) {
      showMsg('#images-local-msg', '删除失败: ' + err.message, false);
    }
  }

  function openMigrateModal(oldImage, users, alternatives) {
    const modal = $('#image-migrate-modal');
    const usersDiv = $('#migrate-users');
    const select = $('#migrate-target');
    const msgEl = $('#migrate-msg');

    msgEl.textContent = '';
    msgEl.className = 'msg';

    // 显示依赖用户
    usersDiv.innerHTML = users.map(u => '<span class="tag">' + esc(u) + '</span>').join(' ');

    // 填充目标镜像下拉
    select.innerHTML = '';
    if (alternatives.length === 0) {
      select.innerHTML = '<option value="">无可用目标镜像</option>';
    } else {
      for (const alt of alternatives) {
        const opt = document.createElement('option');
        opt.value = alt;
        opt.textContent = alt;
        select.appendChild(opt);
      }
    }

    modal.classList.remove('hidden');

    // 绑定确认按钮
    const confirmBtn = $('#migrate-confirm-btn');
    const newConfirmBtn = confirmBtn.cloneNode(true);
    confirmBtn.parentNode.replaceChild(newConfirmBtn, confirmBtn);
    newConfirmBtn.addEventListener('click', async () => {
      const target = select.value;
      if (!target) {
        showMsg('#migrate-msg', '请选择目标镜像', false);
        return;
      }
      newConfirmBtn.disabled = true;
      newConfirmBtn.textContent = '迁移中...';
      showMsg('#migrate-msg', '正在迁移用户并重建容器...', true);

      try {
        // 1. 迁移用户
        const res = await Api.post('/api/admin/images/migrate', {
          image: oldImage,
          target: target,
          users: users.join(','),
        });
        if (!res.success) {
          showMsg('#migrate-msg', res.error || res.message || '迁移失败', false);
          newConfirmBtn.disabled = false;
          newConfirmBtn.textContent = '迁移并删除旧镜像';
          return;
        }

        // 2. 删除旧镜像
        const csrf = await getCSRF();
        const formData = new FormData();
        formData.append('image', oldImage);
        formData.append('csrf_token', csrf);
        const serverBase = await getServerUrl();
        const delResp = await fetch(serverBase + '/api/admin/images/delete', {
          method: 'POST',
          body: formData,
          credentials: 'include',
        });
        const delData = await delResp.json();

        showMsg('#migrate-msg', (res.message || '迁移成功') + (delData.success ? '，旧镜像已删除' : '，删除旧镜像: ' + (delData.error || '未知错误')), delData.success);
        await loadLocalImages();
        loadRegistryTags();
        setTimeout(() => closeMigrateModal(), 2000);
      } catch (err) {
        showMsg('#migrate-msg', err.message, false);
        newConfirmBtn.disabled = false;
        newConfirmBtn.textContent = '迁移并删除旧镜像';
      }
    });
  }

  function closePullModal() {
    $('#image-pull-modal')?.classList.add('hidden');
  }

  function closeMigrateModal() {
    $('#image-migrate-modal')?.classList.add('hidden');
  }

  async function showImageUsers(imageTag) {
    const modal = $('#image-users-modal');
    const title = $('#image-users-title');
    const list = $('#image-users-list');

    title.textContent = imageTag;
    list.innerHTML = '<small>加载中...</small>';
    modal.classList.remove('hidden');

    try {
      const data = await Api.get('/api/admin/images/users?image=' + encodeURIComponent(imageTag));
      const users = data.users || [];
      if (users.length === 0) {
        list.innerHTML = '<small class="text-muted">无依赖用户</small>';
      } else {
        list.innerHTML = users.map(u => '<span class="tag">' + esc(u) + '</span>').join('');
      }
    } catch (e) {
      list.innerHTML = '<small style="color:var(--err)">加载失败: ' + esc(e.message) + '</small>';
    }
  }

  function closeUsersModal() {
    $('#image-users-modal')?.classList.add('hidden');
  }

  let upgradeTag = '';
  let upgradeUsersData = [];

  async function openUpgradeModal(tag) {
    upgradeTag = tag;
    const modal = $('#image-upgrade-modal');
    const targetEl = $('#upgrade-target');
    const groupsDiv = $('#upgrade-groups');
    const usersDiv = $('#upgrade-users');
    const progressDiv = $('#upgrade-progress');
    const msgEl = $('#upgrade-msg');

    targetEl.textContent = tag;
    groupsDiv.innerHTML = '<small>加载中...</small>';
    usersDiv.innerHTML = '<small>加载中...</small>';
    progressDiv.style.display = 'none';
    progressDiv.innerHTML = '';
    msgEl.textContent = '';
    msgEl.className = 'msg';

    modal.classList.remove('hidden');

    try {
      const data = await Api.get('/api/admin/images/upgrade-candidates?tag=' + encodeURIComponent(tag));
      upgradeUsersData = data.users || [];

      // 渲染分组按钮
      const groups = data.groups || [];
      groupsDiv.innerHTML = '';
      if (groups.length === 0) {
        groupsDiv.innerHTML = '<small class="text-muted">无可升级分组</small>';
      } else {
        for (const g of groups) {
          const btn = document.createElement('button');
          btn.className = 'btn btn-sm btn-outline';
          btn.textContent = g.name + ' (' + g.count + ')';
          btn.addEventListener('click', () => toggleGroupUsers(g.name));
          groupsDiv.appendChild(btn);
        }
      }

      // 渲染用户复选框
      renderUpgradeUsers(upgradeUsersData);
    } catch (e) {
      groupsDiv.innerHTML = '<small style="color:var(--err)">加载失败</small>';
      usersDiv.innerHTML = '';
    }

    // 绑定确认按钮
    const confirmBtn = $('#upgrade-confirm-btn');
    const newBtn = confirmBtn.cloneNode(true);
    confirmBtn.parentNode.replaceChild(newBtn, confirmBtn);
    newBtn.addEventListener('click', () => confirmUpgrade());
  }

  function renderUpgradeUsers(users) {
    const usersDiv = $('#upgrade-users');
    usersDiv.innerHTML = '';
    if (users.length === 0) {
      usersDiv.innerHTML = '<small class="text-muted">所有用户已是最新版本</small>';
      return;
    }
    for (const u of users) {
      const label = document.createElement('label');
      label.style.cssText = 'display:inline-flex;align-items:center;gap:4px;padding:4px 8px;border-radius:4px;background:var(--bg-card,#16213e);font-size:12px;cursor:pointer;color:#e0e0e0';
      const cb = document.createElement('input');
      cb.type = 'checkbox';
      cb.value = u.username;
      cb.checked = true;
      label.appendChild(cb);
      label.appendChild(document.createTextNode(u.username + (u.groups ? ' (' + u.groups + ')' : '')));
      usersDiv.appendChild(label);
    }
  }

  function toggleGroupUsers(groupName) {
    const checkboxes = document.querySelectorAll('#upgrade-users input[type=checkbox]');
    checkboxes.forEach(cb => {
      const user = upgradeUsersData.find(u => u.username === cb.value);
      if (user && user.groups && user.groups.split(', ').includes(groupName)) {
        cb.checked = !cb.checked;
      }
    });
  }

  async function confirmUpgrade() {
    const checkboxes = document.querySelectorAll('#upgrade-users input[type=checkbox]:checked');
    const selectedUsers = Array.from(checkboxes).map(cb => cb.value);
    if (selectedUsers.length === 0) {
      showMsg('#upgrade-msg', '请选择要升级的用户', false);
      return;
    }

    const confirmBtn = $('#upgrade-confirm-btn');
    confirmBtn.disabled = true;
    confirmBtn.textContent = '升级中...';

    const progressDiv = $('#upgrade-progress');
    progressDiv.style.display = 'block';
    progressDiv.innerHTML = '';

    const csrf = await getCSRF();
    const serverBase = await getServerUrl();
    const formData = new FormData();
    formData.append('tag', upgradeTag);
    formData.append('users', selectedUsers.join(','));
    formData.append('csrf_token', csrf);

    try {
      const response = await fetch(serverBase + '/api/admin/images/upgrade', {
        method: 'POST',
        body: formData,
        credentials: 'include',
      });

      if (!response.ok && !response.headers.get('content-type')?.includes('text/event-stream')) {
        const data = await response.json();
        showMsg('#upgrade-msg', data.error || '升级失败', false);
        confirmBtn.disabled = false;
        confirmBtn.textContent = '拉取并升级';
        return;
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';
        for (const line of lines) {
          if (line.startsWith('data: ')) {
            const raw = line.slice(6);
            try {
              const obj = JSON.parse(raw);
              if (obj.status === 'done') {
                progressDiv.innerHTML += '<div style="color:#2ecc71">' + esc(obj.message || '升级完成!') + '</div>';
                await loadLocalImages();
                loadRegistryTags();
                break;
              }
              if (obj.status === 'error') {
                progressDiv.innerHTML += '<div style="color:#e74c3c">错误: ' + esc(obj.error || '') + '</div>';
              } else if (obj.status) {
                progressDiv.innerHTML += '<div>' + esc(obj.status) + '</div>';
              }
            } catch {
              progressDiv.innerHTML += '<div>' + esc(raw) + '</div>';
            }
            progressDiv.scrollTop = progressDiv.scrollHeight;
          }
        }
      }
    } catch (err) {
      progressDiv.innerHTML += '<div style="color:#e74c3c">错误: ' + esc(err.message) + '</div>';
    }
    confirmBtn.disabled = false;
    confirmBtn.textContent = '拉取并升级';
  }

  function closeUpgradeModal() {
    $('#image-upgrade-modal')?.classList.add('hidden');
  }
}
