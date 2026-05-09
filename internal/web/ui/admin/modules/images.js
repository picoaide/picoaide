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
  let upgradeGroupsData = [];
  let upgradeSelectedGroups = new Set();

  async function openUpgradeModal(tag) {
    upgradeTag = tag;
    upgradeSelectedGroups = new Set();
    const modal = $('#image-upgrade-modal');
    const targetEl = $('#upgrade-target');
    const usersDiv = $('#upgrade-users');
    const progressDiv = $('#upgrade-progress');
    const msgEl = $('#upgrade-msg');
    const groupSelect = $('#upgrade-group-select');

    targetEl.textContent = tag;
    usersDiv.innerHTML = '<small style="color:#666">加载中...</small>';
    progressDiv.style.display = 'none';
    progressDiv.innerHTML = '';
    msgEl.textContent = '';
    msgEl.className = 'msg';
    $('#upgrade-selected-groups').innerHTML = '';
    $('#upgrade-search').value = '';
    $('#upgrade-count').textContent = '';

    modal.classList.remove('hidden');

    try {
      const data = await Api.get('/api/admin/images/upgrade-candidates?tag=' + encodeURIComponent(tag));
      upgradeUsersData = data.users || [];
      upgradeGroupsData = data.groups || [];

      // 填充分组下拉
      groupSelect.innerHTML = '<option value="">-- 选择分组 --</option>';
      for (const g of upgradeGroupsData) {
        const opt = document.createElement('option');
        opt.value = g.name;
        opt.textContent = g.name + ' (' + g.count + ' 人)';
        groupSelect.appendChild(opt);
      }

      if (upgradeUsersData.length === 0) {
        usersDiv.innerHTML = '<small style="color:#666">所有用户已是最新版本</small>';
      } else {
        renderUpgradeTable(upgradeUsersData);
      }
    } catch (e) {
      usersDiv.innerHTML = '<small style="color:var(--err)">加载失败: ' + esc(e.message) + '</small>';
    }

    // 绑定搜索
    const searchInput = $('#upgrade-search');
    const newSearch = searchInput.cloneNode(true);
    searchInput.parentNode.replaceChild(newSearch, searchInput);
    newSearch.addEventListener('input', () => {
      const q = newSearch.value.toLowerCase().trim();
      const filtered = q ? upgradeUsersData.filter(u =>
        u.username.toLowerCase().includes(q) || (u.groups && u.groups.toLowerCase().includes(q))
      ) : upgradeUsersData;
      renderUpgradeTable(filtered);
    });

    // 绑定添加分组按钮
    const addGroupBtn = $('#upgrade-add-group');
    const newAddGroup = addGroupBtn.cloneNode(true);
    addGroupBtn.parentNode.replaceChild(newAddGroup, addGroupBtn);
    newAddGroup.addEventListener('click', () => {
      const name = $('#upgrade-group-select').value;
      if (!name || upgradeSelectedGroups.has(name)) return;
      upgradeSelectedGroups.add(name);
      selectGroupUsers(name, true);
      renderSelectedGroups();
    });

    // 绑定全员升级按钮
    const allBtn = $('#upgrade-all-btn');
    const newAllBtn = allBtn.cloneNode(true);
    allBtn.parentNode.replaceChild(newAllBtn, allBtn);
    newAllBtn.addEventListener('click', () => {
      document.querySelectorAll('#upgrade-users tbody input[type=checkbox]').forEach(cb => cb.checked = true);
      updateUpgradeCount();
    });

    // 绑定全选/清空
    const selectAllBtn = $('#upgrade-select-all');
    const newSelectAll = selectAllBtn.cloneNode(true);
    selectAllBtn.parentNode.replaceChild(newSelectAll, selectAllBtn);
    newSelectAll.addEventListener('click', () => {
      document.querySelectorAll('#upgrade-users input[type=checkbox]').forEach(cb => cb.checked = true);
      updateUpgradeCount();
    });

    const deselectAllBtn = $('#upgrade-deselect-all');
    const newDeselectAll = deselectAllBtn.cloneNode(true);
    deselectAllBtn.parentNode.replaceChild(newDeselectAll, deselectAllBtn);
    newDeselectAll.addEventListener('click', () => {
      document.querySelectorAll('#upgrade-users input[type=checkbox]').forEach(cb => cb.checked = false);
      upgradeSelectedGroups.clear();
      renderSelectedGroups();
      updateUpgradeCount();
    });

    // 绑定确认按钮
    const confirmBtn = $('#upgrade-confirm-btn');
    const newBtn = confirmBtn.cloneNode(true);
    confirmBtn.parentNode.replaceChild(newBtn, confirmBtn);
    newBtn.addEventListener('click', () => confirmUpgrade());
  }

  function renderUpgradeTable(users) {
    const usersDiv = $('#upgrade-users');
    usersDiv.innerHTML = '';
    if (users.length === 0) {
      usersDiv.innerHTML = '<div style="padding:12px;color:#999;text-align:center">无匹配用户</div>';
      updateUpgradeCount();
      return;
    }
    const table = document.createElement('table');
    table.style.cssText = 'width:100%;border-collapse:collapse;font-size:12px;background:#fff';
    const thead = document.createElement('thead');
    thead.innerHTML = '<tr style="border-bottom:2px solid #ddd;background:#f5f5f5">' +
      '<th style="padding:6px 8px;text-align:left;width:30px"><input type="checkbox" id="upgrade-header-cb"></th>' +
      '<th style="padding:6px 8px;text-align:left;color:#555">用户名</th>' +
      '<th style="padding:6px 8px;text-align:left;color:#555">当前版本</th>' +
      '<th style="padding:6px 8px;text-align:left;color:#555">状态</th>' +
      '<th style="padding:6px 8px;text-align:left;color:#555">分组</th></tr>';
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    for (const u of users) {
      const tr = document.createElement('tr');
      tr.style.cssText = 'border-bottom:1px solid #eee;background:#fff';
      tr.innerHTML =
        '<td style="padding:4px 8px"><input type="checkbox" value="' + esc(u.username) + '" checked></td>' +
        '<td style="padding:4px 8px;color:#333">' + esc(u.username) + '</td>' +
        '<td style="padding:4px 8px;color:#666;font-family:monospace;font-size:11px">' + esc(u.image.split(':').pop()) + '</td>' +
        '<td style="padding:4px 8px">' + (u.status === 'running' ? '<span style="color:#27ae60">运行中</span>' : '<span style="color:#999">' + esc(u.status) + '</span>') + '</td>' +
        '<td style="padding:4px 8px;color:#666">' + esc(u.groups || '-') + '</td>';
      tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    usersDiv.appendChild(table);

    // 表头全选/取消
    const headerCb = document.getElementById('upgrade-header-cb');
    if (headerCb) {
      headerCb.addEventListener('change', () => {
        tbody.querySelectorAll('input[type=checkbox]').forEach(cb => cb.checked = headerCb.checked);
        updateUpgradeCount();
      });
    }

    // 单个变更时更新计数
    tbody.querySelectorAll('input[type=checkbox]').forEach(cb => {
      cb.addEventListener('change', updateUpgradeCount);
    });

    updateUpgradeCount();
  }

  function updateUpgradeCount() {
    const all = document.querySelectorAll('#upgrade-users tbody input[type=checkbox]').length;
    const checked = document.querySelectorAll('#upgrade-users tbody input[type=checkbox]:checked').length;
    $('#upgrade-count').textContent = checked + ' / ' + all + ' 人已选';
  }

  function selectGroupUsers(groupName, checked) {
    const rows = document.querySelectorAll('#upgrade-users tbody tr');
    rows.forEach(tr => {
      const cb = tr.querySelector('input[type=checkbox]');
      if (!cb) return;
      const user = upgradeUsersData.find(u => u.username === cb.value);
      if (user && user.groups && user.groups.split(', ').includes(groupName)) {
        cb.checked = checked;
      }
    });
    updateUpgradeCount();
  }

  function renderSelectedGroups() {
    const container = $('#upgrade-selected-groups');
    container.innerHTML = '';
    for (const name of upgradeSelectedGroups) {
      const tag = document.createElement('span');
      tag.style.cssText = 'display:inline-flex;align-items:center;gap:4px;padding:2px 8px;border-radius:4px;background:#e8f4fd;color:#333;font-size:12px;border:1px solid #b3d9f2';
      tag.textContent = name;
      const x = document.createElement('span');
      x.style.cssText = 'cursor:pointer;color:#c0392b;font-weight:bold;margin-left:4px';
      x.textContent = '×';
      x.addEventListener('click', () => {
        upgradeSelectedGroups.delete(name);
        selectGroupUsers(name, false);
        renderSelectedGroups();
      });
      tag.appendChild(x);
      container.appendChild(tag);
    }
  }

  async function confirmUpgrade() {
    const checkboxes = document.querySelectorAll('#upgrade-users tbody input[type=checkbox]:checked');
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

      // 读取 SSE 流（拉取镜像阶段）
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      let taskId = null;

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
                taskId = obj.task_id;
                progressDiv.innerHTML += '<div style="color:#2ecc71">' + esc(obj.message) + '</div>';
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

      // 如果有任务 ID，轮询队列进度
      if (taskId) {
        pollUpgradeProgress(progressDiv);
      }
    } catch (err) {
      progressDiv.innerHTML += '<div style="color:#e74c3c">错误: ' + esc(err.message) + '</div>';
    }
    confirmBtn.disabled = false;
    confirmBtn.textContent = '拉取并升级';
  }

  async function pollUpgradeProgress(progressDiv) {
    const serverBase = await getServerUrl();
    const poll = async () => {
      try {
        const data = await Api.get('/api/admin/task/status');
        const s = data.status || data;
        const lineId = 'upgrade-poll-status';
        let el = document.getElementById(lineId);
        if (!el) {
          el = document.createElement('div');
          el.id = lineId;
          progressDiv.appendChild(el);
        }
        const pct = s.total > 0 ? Math.round(s.done / s.total * 100) : 0;
        el.textContent = '升级进度：' + s.done + '/' + s.total + ' (' + pct + '%)' +
          (s.failed > 0 ? '，失败 ' + s.failed : '') +
          (s.message ? ' — ' + s.message : '');
        progressDiv.scrollTop = progressDiv.scrollHeight;

        if (s.running) {
          setTimeout(poll, 2000);
        } else {
          el.style.color = '#2ecc71';
          el.textContent = '升级完成：' + s.done + ' 成功' + (s.failed > 0 ? '，' + s.failed + ' 失败' : '');
          await loadLocalImages();
          loadRegistryTags();
        }
      } catch {
        setTimeout(poll, 3000);
      }
    };
    setTimeout(poll, 1000);
  }

  function closeUpgradeModal() {
    $('#image-upgrade-modal')?.classList.add('hidden');
  }
}
