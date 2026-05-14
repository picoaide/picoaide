export async function init(ctx) {
  const { Api, esc, showMsg, $ } = ctx;

  let localImages = [];
  let imageUsersTag = '';
  let imageUsersPage = 1;
  let imageUsersSearch = '';
  let imageUsersTotalPages = 1;
  let isPulling = false;
  let pullingTag = '';

  await loadLocalImages();
  loadRegistryTags();

  $('#images-refresh-registry')?.addEventListener('click', loadRegistryTags);
  $('#pull-modal-close')?.addEventListener('click', closePullModal);
  $('#pull-close-btn')?.addEventListener('click', closePullModal);
  $('#migrate-modal-close')?.addEventListener('click', closeMigrateModal);
  $('#migrate-cancel-btn')?.addEventListener('click', closeMigrateModal);
  $('#image-users-close')?.addEventListener('click', closeUsersModal);
  $('#image-users-close-btn')?.addEventListener('click', closeUsersModal);
  $('#image-users-search-btn')?.addEventListener('click', () => {
    imageUsersSearch = $('#image-users-search').value.trim();
    imageUsersPage = 1;
    loadImageUsers();
  });
  $('#image-users-search')?.addEventListener('keydown', e => {
    if (e.key === 'Enter') {
      imageUsersSearch = e.target.value.trim();
      imageUsersPage = 1;
      loadImageUsers();
    }
  });
  $('#image-users-prev')?.addEventListener('click', () => {
    if (imageUsersPage > 1) {
      imageUsersPage--;
      loadImageUsers();
    }
  });
  $('#image-users-next')?.addEventListener('click', () => {
    if (imageUsersPage < imageUsersTotalPages) {
      imageUsersPage++;
      loadImageUsers();
    }
  });
  $('#upgrade-modal-close')?.addEventListener('click', closeUpgradeModal);
  $('#upgrade-cancel-btn')?.addEventListener('click', closeUpgradeModal);
  $('#upgrade-select-all')?.addEventListener('click', () => {
    document.querySelectorAll('#upgrade-users input[type=checkbox]').forEach(cb => cb.checked = true);
  });
  $('#upgrade-deselect-all')?.addEventListener('click', () => {
    document.querySelectorAll('#upgrade-users input[type=checkbox]').forEach(cb => cb.checked = false);
  });

  // 拉取状态轮询（解析 Docker 进度 JSON，显示百分比，拉取完毕刷新一次列表）
  let prevPullRunning = false;
  function parsePullProgress(msg) {
    try {
      var d = JSON.parse(msg);
      var pct = '';
      if (d.progressDetail && d.progressDetail.total > 0) {
        pct = Math.round(d.progressDetail.current / d.progressDetail.total * 100) + '%';
      }
      return (d.id || '') + ' ' + (d.status || '') + (pct ? ' ' + pct : '');
    } catch (e) {
      return msg;
    }
  }
  async function checkPullStatus() {
    const data = await Api.get('/api/admin/images/pull-status').catch(function() { return {}; });
    if (data.running) {
      prevPullRunning = true;
      $('#image-pull-indicator')?.classList.remove('hidden');
      $('#image-pull-indicator .pull-tag').textContent = data.tag || '';
      $('#image-pull-indicator .pull-msg').textContent = parsePullProgress(data.message);
      $('#image-pull-indicator .pull-time').textContent = data.started_at || '';
    } else {
      $('#image-pull-indicator')?.classList.add('hidden');
      if (prevPullRunning) {
        prevPullRunning = false;
        await loadLocalImages();
        loadRegistryTags();
      }
    }
  }
  setInterval(checkPullStatus, 3000);

  async function loadLocalImages() {
    const data = await Api.get('/api/admin/images').catch(() => ({ images: [], pulling: false }));
    const tbody = $('#images-local');
    tbody.innerHTML = '';
    localImages = data.images || [];
    if (localImages.length === 0) {
      updatePullIndicator(data);
      if (!isPulling) {
        $('#images-local-empty')?.classList.remove('hidden');
      }
      return;
    }
    $('#images-local-empty')?.classList.add('hidden');
    updatePullIndicator(data);
    for (const img of localImages) {
      const tags = (img.repo_tags || []).join(', ') || '(无标签)';
      const userCount = img.user_count || 0;
      const userBtnHtml = userCount > 0
        ? '<button class="btn btn-sm btn-outline" data-users-img="' + esc(tags) + '">' + userCount + ' 个用户</button>'
        : '<small class="text-muted">-</small>';
      const tr = document.createElement('tr');
      const actionsTd = '<td class="actions-cell"><div class="btn-group"></div></td>';
      tr.innerHTML =
        '<td style="font-family:monospace;font-size:12px;word-break:break-all">' + esc(tags) + '</td>' +
        '<td style="font-family:monospace;font-size:12px">' + esc(img.id || '-') + '</td>' +
        '<td>' + esc(img.size_str) + '</td>' +
        '<td style="white-space:nowrap">' + esc(img.created_str || '-') + '</td>' +
        '<td>' + userBtnHtml + '</td>' +
        actionsTd;
      for (const tag of (img.repo_tags || [])) {
        const changeBtn = document.createElement('button');
        changeBtn.className = 'btn btn-sm btn-primary';
        changeBtn.textContent = '变更版本';
        changeBtn.dataset.change = tag;
        changeBtn.addEventListener('click', () => openUpgradeModal(tag));
        tr.querySelector('td:last-child .btn-group').appendChild(changeBtn);
        const delBtn = document.createElement('button');
        delBtn.className = 'btn btn-sm btn-danger';
        delBtn.textContent = '删除';
        delBtn.dataset.image = tag;
        delBtn.addEventListener('click', () => deleteImage(tag));
        tr.querySelector('td:last-child .btn-group').appendChild(delBtn);
      }
      tbody.appendChild(tr);
    }
    tbody.querySelectorAll('[data-users-img]').forEach(btn => {
      btn.addEventListener('click', () => showImageUsers(btn.dataset.usersImg));
    });
  }

  function updatePullIndicator(data) {
    const indicator = $('#image-pull-indicator');
    if (!indicator) return;
    isPulling = data.pulling || false;
    pullingTag = data.pulling_tag || '';
    if (isPulling) {
      indicator.classList.remove('hidden');
      indicator.querySelector('.pull-tag').textContent = pullingTag;
      indicator.querySelector('.pull-msg').textContent = data.pull_status?.message || '正在拉取...';
      indicator.querySelector('.pull-time').textContent = data.pull_status?.started_at || '';
    } else {
      indicator.classList.add('hidden');
    }
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
      const isCurrentPull = isPulling && pullingTag === tag;
      const tr = document.createElement('tr');
      let statusHtml;
      if (isCurrentPull) {
        statusHtml = '<span class="badge badge-warn">拉取中...</span>';
      } else if (exists) {
        statusHtml = '<span class="badge badge-ok">已存在</span>';
      } else {
        statusHtml = '<span class="badge badge-muted">未拉取</span>';
      }
      const pullDisabled = isPulling ? ' disabled' : '';
      tr.innerHTML =
        '<td style="font-family:monospace">' + esc(tag) + '</td>' +
        '<td>' + statusHtml + '</td>' +
        '<td class="actions-cell"><div class="btn-group"><button class="btn btn-sm btn-outline"' + pullDisabled + ' data-tag="' + esc(tag) + '">' + (isCurrentPull ? '拉取中...' : (exists ? '重新拉取' : '拉取')) + '</button></div></td>';
      tbody.appendChild(tr);
    }
    tbody.querySelectorAll('[data-tag]').forEach(btn => {
      btn.addEventListener('click', () => pullImage(btn.dataset.tag));
    });
  }

  async function pullImage(tag) {
    const res = await Api.post('/api/admin/images/pull', { tag: tag });
    if (!res.success) {
      showMsg('#images-local-msg', res.error || '拉取失败', false);
      return;
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

    imageUsersTag = imageTag;
    imageUsersPage = 1;
    imageUsersSearch = '';
    $('#image-users-search').value = '';
    title.textContent = imageTag;
    modal.classList.remove('hidden');
    loadImageUsers();
  }

  async function loadImageUsers() {
    const list = $('#image-users-list');
    list.innerHTML = '<small>加载中...</small>';

    try {
      const params = new URLSearchParams({
        image: imageUsersTag,
        page: String(imageUsersPage),
        page_size: '50',
      });
      if (imageUsersSearch) params.set('search', imageUsersSearch);
      const data = await Api.get('/api/admin/images/users?' + params.toString());
      imageUsersPage = data.page || imageUsersPage;
      imageUsersTotalPages = data.total_pages || 1;
      const users = data.users || [];
      if (users.length === 0) {
        list.innerHTML = '<small class="text-muted">无依赖用户</small>';
      } else {
        list.innerHTML = users.map(u => '<span class="tag">' + esc(u) + '</span>').join('');
      }
      const info = $('#image-users-page-info');
      if (info) info.textContent = '第 ' + imageUsersPage + ' / ' + imageUsersTotalPages + ' 页，共 ' + (data.total || 0) + ' 个用户';
      $('#image-users-prev').disabled = imageUsersPage <= 1;
      $('#image-users-next').disabled = imageUsersPage >= imageUsersTotalPages;
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
  let upgradeSelectedUsers = new Set();
  let upgradePage = 1;
  let upgradeTotalPages = 1;
  let upgradeSearch = '';

  async function openUpgradeModal(tag) {
    upgradeTag = tag;
    upgradeSelectedGroups = new Set();
    upgradeSelectedUsers = new Set();
    upgradePage = 1;
    upgradeSearch = '';
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
      const data = await loadUpgradeCandidates();
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
      upgradeSearch = newSearch.value.trim();
      upgradePage = 1;
      refreshUpgradeUsers();
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
      upgradeUsersData.forEach(u => upgradeSelectedUsers.add(u.username));
      updateUpgradeCount();
    });

    // 绑定全选/清空
    const selectAllBtn = $('#upgrade-select-all');
    const newSelectAll = selectAllBtn.cloneNode(true);
    selectAllBtn.parentNode.replaceChild(newSelectAll, selectAllBtn);
    newSelectAll.addEventListener('click', () => {
      document.querySelectorAll('#upgrade-users input[type=checkbox]').forEach(cb => cb.checked = true);
      upgradeUsersData.forEach(u => upgradeSelectedUsers.add(u.username));
      updateUpgradeCount();
    });

    const deselectAllBtn = $('#upgrade-deselect-all');
    const newDeselectAll = deselectAllBtn.cloneNode(true);
    deselectAllBtn.parentNode.replaceChild(newDeselectAll, deselectAllBtn);
    newDeselectAll.addEventListener('click', () => {
      document.querySelectorAll('#upgrade-users input[type=checkbox]').forEach(cb => cb.checked = false);
      upgradeSelectedGroups.clear();
      upgradeSelectedUsers.clear();
      renderSelectedGroups();
      updateUpgradeCount();
    });

    // 绑定确认按钮
    const confirmBtn = $('#upgrade-confirm-btn');
    const newBtn = confirmBtn.cloneNode(true);
    confirmBtn.parentNode.replaceChild(newBtn, confirmBtn);
    newBtn.addEventListener('click', () => confirmUpgrade());
    bindUpgradePager();
  }

  async function loadUpgradeCandidates() {
    const params = new URLSearchParams({
      tag: upgradeTag,
      page: String(upgradePage),
      page_size: '50',
    });
    if (upgradeSearch) params.set('search', upgradeSearch);
    const data = await Api.get('/api/admin/images/upgrade-candidates?' + params.toString());
    upgradePage = data.page || upgradePage;
    upgradeTotalPages = data.total_pages || 1;
    const info = $('#upgrade-page-info');
    if (info) info.textContent = '第 ' + upgradePage + ' / ' + upgradeTotalPages + ' 页，共 ' + (data.total || 0) + ' 个候选用户';
    $('#upgrade-prev').disabled = upgradePage <= 1;
    $('#upgrade-next').disabled = upgradePage >= upgradeTotalPages;
    return data;
  }

  async function refreshUpgradeUsers() {
    const usersDiv = $('#upgrade-users');
    usersDiv.innerHTML = '<small style="color:#666">加载中...</small>';
    try {
      const data = await loadUpgradeCandidates();
      upgradeUsersData = data.users || [];
      renderUpgradeTable(upgradeUsersData);
    } catch (e) {
      usersDiv.innerHTML = '<small style="color:var(--err)">加载失败: ' + esc(e.message) + '</small>';
    }
  }

  function bindUpgradePager() {
    const prev = $('#upgrade-prev');
    const next = $('#upgrade-next');
    const newPrev = prev.cloneNode(true);
    const newNext = next.cloneNode(true);
    prev.parentNode.replaceChild(newPrev, prev);
    next.parentNode.replaceChild(newNext, next);
    newPrev.addEventListener('click', () => {
      if (upgradePage > 1) {
        upgradePage--;
        refreshUpgradeUsers();
      }
    });
    newNext.addEventListener('click', () => {
      if (upgradePage < upgradeTotalPages) {
        upgradePage++;
        refreshUpgradeUsers();
      }
    });
  }

  function renderUpgradeTable(users) {
    const usersDiv = $('#upgrade-users');
    usersDiv.innerHTML = '';
    usersDiv.classList.add('table-wrap');
    usersDiv.style.overflow = 'auto';
    if (users.length === 0) {
      usersDiv.innerHTML = '<div style="padding:12px;color:#999;text-align:center">无匹配用户</div>';
      updateUpgradeCount();
      return;
    }
    const table = document.createElement('table');
    table.className = 'compact-table';
    table.style.cssText = 'width:100%;border-collapse:collapse;font-size:12px;background:#fff';
    const thead = document.createElement('thead');
    thead.innerHTML = '<tr style="border-bottom:2px solid #ddd;background:#f5f5f5">' +
      '<th style="padding:6px 8px;text-align:left;width:52px"><label class="toggle-switch toggle-switch-compact"><input type="checkbox" id="upgrade-header-cb"><span class="toggle-switch-control" aria-hidden="true"></span></label></th>' +
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
        '<td style="padding:4px 8px"><label class="toggle-switch toggle-switch-compact"><input type="checkbox" value="' + esc(u.username) + '"' + (upgradeSelectedUsers.has(u.username) ? ' checked' : '') + '><span class="toggle-switch-control" aria-hidden="true"></span></label></td>' +
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
      cb.addEventListener('change', () => {
        if (cb.checked) upgradeSelectedUsers.add(cb.value);
        else upgradeSelectedUsers.delete(cb.value);
        updateUpgradeCount();
      });
    });

    updateUpgradeCount();
  }

  function updateUpgradeCount() {
    const all = document.querySelectorAll('#upgrade-users tbody input[type=checkbox]').length;
    const checked = document.querySelectorAll('#upgrade-users tbody input[type=checkbox]:checked').length;
    $('#upgrade-count').textContent = '本页 ' + checked + ' / ' + all + '，已选 ' + upgradeSelectedUsers.size + ' 人';
  }

  function selectGroupUsers(groupName, checked) {
    const rows = document.querySelectorAll('#upgrade-users tbody tr');
    rows.forEach(tr => {
      const cb = tr.querySelector('input[type=checkbox]');
      if (!cb) return;
      const user = upgradeUsersData.find(u => u.username === cb.value);
      if (user && user.groups && user.groups.split(', ').includes(groupName)) {
        cb.checked = checked;
        if (checked) upgradeSelectedUsers.add(cb.value);
        else upgradeSelectedUsers.delete(cb.value);
      }
    });
    updateUpgradeCount();
  }

  function renderSelectedGroups() {
    const container = $('#upgrade-selected-groups');
    container.innerHTML = '';
    for (const name of upgradeSelectedGroups) {
      const tag = document.createElement('span');
      tag.className = 'tag';
      tag.textContent = name;
      const x = document.createElement('span');
      x.style.cssText = 'cursor:pointer;color:var(--danger);font-weight:bold;margin-left:4px';
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
    const selectedUsers = Array.from(upgradeSelectedUsers);
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
