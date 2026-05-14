export async function init(ctx) {
  const { Api, esc, showMsg } = ctx;

  let page = 1;
  let pageSize = 50;
  let totalPages = 1;
  let search = '';
  let unifiedAuth = true;
  let allGroups = [];

  ctx.$('#refresh-groups').addEventListener('click', loadGroups);
  ctx.$('#create-group-btn')?.addEventListener('click', openCreateGroupModal);
  ctx.$('#groups-search-btn')?.addEventListener('click', () => {
    search = ctx.$('#groups-search').value.trim();
    page = 1;
    loadGroups();
  });
  ctx.$('#groups-search')?.addEventListener('keydown', e => {
    if (e.key === 'Enter') {
      search = e.target.value.trim();
      page = 1;
      loadGroups();
    }
  });
  ctx.$('#groups-page-size')?.addEventListener('change', e => {
    pageSize = Number(e.target.value) || 50;
    page = 1;
    loadGroups();
  });
  ctx.$('#groups-prev')?.addEventListener('click', () => {
    if (page > 1) {
      page--;
      loadGroups();
    }
  });
  ctx.$('#groups-next')?.addEventListener('click', () => {
    if (page < totalPages) {
      page++;
      loadGroups();
    }
  });

  await loadGroups();

  async function loadGroups() {
    const tbody = ctx.$('#groups-tbody');
    tbody.innerHTML = '<tr><td colspan="5" class="text-center">加载中...</td></tr>';
    ctx.$('#groups-empty').classList.add('hidden');

    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
    if (search) params.set('search', search);
    const data = await Api.get('/api/admin/groups?' + params.toString());
    if (!data.success) { tbody.innerHTML = ''; return; }
    page = data.page || page;
    pageSize = data.page_size || pageSize;
    totalPages = data.total_pages || 1;
    unifiedAuth = !!data.unified_auth;
    const groups = data.groups || [];
    allGroups = groups;
    updateModeNotice();
    updatePager(data);

    if (groups.length === 0) { tbody.innerHTML = ''; ctx.$('#groups-empty').classList.remove('hidden'); return; }

    // 构建树
    const byParent = {};
    const byId = {};
    for (const g of groups) {
      byId[g.id] = g;
      const pid = g.parent_id || 'root';
      if (!byParent[pid]) byParent[pid] = [];
      byParent[pid].push(g);
    }

    tbody.innerHTML = '';
    function renderTree(parentId, depth) {
      const children = byParent[parentId] || [];
      for (const g of children) {
        const tr = document.createElement('tr');
        const indent = '\u00A0\u00A0'.repeat(depth) + (depth > 0 ? '└ ' : '');
        const srcBadge = g.source === 'ldap' ? '<span class="badge badge-ok">LDAP</span>' : '<span class="badge badge-muted">本地</span>';
        const subCount = (byParent[g.id] || []).length;
        const subInfo = subCount > 0 ? ' <small class="text-muted">(' + subCount + ' 个子组)</small>' : '';
        const deleteBtn = !unifiedAuth ? '<button class="btn btn-sm btn-danger" data-group-delete="' + esc(g.name) + '">删除</button>' : '';
        tr.innerHTML = '<td><strong>' + indent + esc(g.name) + '</strong>' + subInfo + '</td><td>' + srcBadge + '</td><td>' + g.member_count + '</td><td class="actions-cell"><div class="btn-group"><button class="btn btn-sm btn-outline" data-group-detail="' + esc(g.name) + '">详情</button><button class="btn btn-sm btn-outline" data-group-apply="' + esc(g.name) + '">下发配置</button>' + deleteBtn + '</div></td>';
        tbody.appendChild(tr);
        renderTree(g.id, depth + 1);
      }
    }
    renderTree('root', 0);

    tbody.querySelectorAll('[data-group-detail]').forEach(btn => {
      btn.addEventListener('click', () => openGroupDetail(btn.dataset.groupDetail));
    });
    tbody.querySelectorAll('[data-group-apply]').forEach(btn => {
      btn.addEventListener('click', async () => {
        if (!await confirmModal('确定要下发配置到组 ' + btn.dataset.groupApply + ' 的所有成员（包含子组成员）并重启容器吗？')) return;
        showMsg('#groups-msg', '提交中...', true);
        try {
          const res = await Api.post('/api/admin/config/apply', { group: btn.dataset.groupApply });
          if (res.task_id) {
            pollGroupsTask('#groups-msg', '下发配置到组 ' + btn.dataset.groupApply);
          } else {
            showMsg('#groups-msg', res.message || res.error, res.success);
          }
        } catch (e) { showMsg('#groups-msg', e.message, false); }
      });
    });
    tbody.querySelectorAll('[data-group-delete]').forEach(btn => {
      btn.addEventListener('click', async () => {
        if (!await confirmModal('确定删除用户组 ' + btn.dataset.groupDelete + '？组成员关系和技能绑定会一并删除。')) return;
        const res = await Api.post('/api/admin/groups/delete', { name: btn.dataset.groupDelete });
        showMsg('#groups-msg', res.message || res.error, res.success);
        if (res.success) loadGroups();
      });
    });
  }

  function updateModeNotice() {
    ctx.$('#create-group-btn')?.classList.toggle('hidden', unifiedAuth);
    const desc = ctx.$('#groups-page-desc');
    if (desc) {
      desc.textContent = unifiedAuth
        ? '用户组来自当前认证源，可用于绑定 Skill、下发配置和管理容器策略。'
        : '用户组由管理员本地维护，可用于绑定 Skill、下发配置和管理容器策略。';
    }
    const tip = ctx.$('#groups-mode-tip');
    if (!tip) return;
    tip.classList.remove('hidden');
    tip.textContent = unifiedAuth
      ? '用户组和成员由当前认证源同步，不支持手动新建、删除或编辑成员。'
      : '当前为本地认证，用户组和成员可在此手动维护。';
    tip.className = 'msg msg-info';
  }

  function updatePager(data) {
    const info = ctx.$('#groups-page-info');
    if (info) info.textContent = '第 ' + (data.page || 1) + ' / ' + (data.total_pages || 1) + ' 页，共 ' + (data.total || 0) + ' 个组';
    const sizeSel = ctx.$('#groups-page-size');
    if (sizeSel) sizeSel.value = String(pageSize);
    const prev = ctx.$('#groups-prev');
    const next = ctx.$('#groups-next');
    if (prev) prev.disabled = page <= 1;
    if (next) next.disabled = page >= totalPages;
  }

  async function openGroupDetail(groupName) {
    ctx.$('#group-modal')?.remove();
    const overlay = document.createElement('div');
    overlay.id = 'group-modal';
    overlay.className = 'modal-overlay';
    let memberPage = 1;
    let memberPageSize = 50;
    let memberTotalPages = 1;
    let memberSearch = '';
    let candidateSearch = '';
    let candidatePage = 1;
    let candidateTotalPages = 1;
    let hasChanges = false;
    let saving = false;
    let members = [];
    let inheritedMembers = [];
    let pendingAdds = new Set();
    let pendingRemoves = new Set();
    let candidateUsers = [];
    let candidateExisting = new Set();

    overlay.innerHTML =
      '<div class="modal" style="max-width:760px">' +
        '<div class="modal-header">组: ' + esc(groupName) + '<button id="modal-close">&times;</button></div>' +
        '<div class="modal-body" style="overflow-y:auto">' +
          (!unifiedAuth ? '<div class="field"><label>添加成员</label><div class="list-search"><input type="search" id="gd-candidate-search" placeholder="搜索可添加的本地用户"><button class="btn btn-sm btn-outline" id="gd-candidate-search-btn">搜索</button></div><div id="gd-candidates" class="mt-1"></div><div class="pager" id="gd-candidates-pager"><span class="pager-info" id="gd-candidates-info"></span><button class="btn btn-sm btn-outline" id="gd-candidates-prev">上一页</button><button class="btn btn-sm btn-outline" id="gd-candidates-next">下一页</button></div></div>' : '') +
          '<div class="field"><label>已加入成员</label>' +
          '<div class="list-search mb-1">' +
            '<input type="search" id="gd-member-search" placeholder="搜索成员用户名">' +
            '<button class="btn btn-sm btn-outline" id="gd-member-search-btn">搜索</button>' +
          '</div>' +
          '<div id="gd-members" class="mb-1"></div>' +
          '<div class="pager" id="gd-members-pager">' +
            '<span class="pager-info" id="gd-members-info"></span>' +
            '<button class="btn btn-sm btn-outline" id="gd-members-prev">上一页</button>' +
            '<button class="btn btn-sm btn-outline" id="gd-members-next">下一页</button>' +
          '</div>' +
          '</div>' +
          '<div id="gd-staging-info" class="mt-1"></div>' +
          '<small class="text-muted">' + (unifiedAuth ? '成员由当前认证源同步' : '成员由管理员本地维护') + '</small>' +
        '</div>' +
        (!unifiedAuth ? '<div class="modal-footer" id="gd-modal-actions"><button class="btn btn-primary" id="gd-save-btn">保存</button><button class="btn btn-outline" id="gd-cancel-btn">取消</button></div>' : '') +
      '</div>';
    ctx.$('#content-area').appendChild(overlay);
    overlay.querySelector('#modal-close').addEventListener('click', async () => {
      if (hasChanges && !await confirmModal('有未保存的更改，确定关闭吗？')) return;
      overlay.remove();
    });
    overlay.addEventListener('click', async e => {
      if (e.target === overlay) {
        if (hasChanges && !await confirmModal('有未保存的更改，确定关闭吗？')) return;
        overlay.remove();
      }
    });

    function updateStagingInfo() {
      const el = overlay.querySelector('#gd-staging-info');
      const parts = [];
      if (pendingAdds.size > 0) parts.push('待添加: ' + pendingAdds.size + ' 人');
      if (pendingRemoves.size > 0) parts.push('待移除: ' + pendingRemoves.size + ' 人');
      hasChanges = pendingAdds.size > 0 || pendingRemoves.size > 0;
      el.innerHTML = hasChanges ? '<div class="msg msg-info">' + parts.join('，') + '</div>' : '';
      const saveBtn = overlay.querySelector('#gd-save-btn');
      if (saveBtn) saveBtn.disabled = !hasChanges || saving;
    }

    function renderMembers() {
      const el = overlay.querySelector('#gd-members');
      let html = '';
      const shown = members.filter(m => !pendingRemoves.has(m) || pendingAdds.has(m));
      if (shown.length === 0 && inheritedMembers.length === 0) html += '<small class="text-muted">暂无成员</small>';
      else {
        html += shown.map(m => {
          if (pendingAdds.has(m)) return '<span class="tag" style="background:#e0f2fe;border-color:#7dd3fc">' + esc(m) + ' <small style="color:#0369a1">待加入</small></span>';
          return '<span class="tag">' + esc(m) + (!unifiedAuth ? ' <button data-rm-member="' + esc(m) + '"' + (pendingRemoves.has(m) ? ' style="color:#dc2626"' : '') + '>&times;</button>' : '') + '</span>';
        }).join(' ');
        if (pendingRemoves.size > 0) {
          const removed = members.filter(m => pendingRemoves.has(m));
          html += '<div class="mt-1"><small class="text-muted">待移除</small><div>' +
            removed.map(m => '<span class="tag" style="text-decoration:line-through;opacity:.6;background:#fef2f2;border-color:#fecaca">' + esc(m) + '</span>').join(' ') +
            '</div></div>';
        }
      }
      if (inheritedMembers.length > 0) {
        html += '<div class="mt-1"><small class="text-muted">子组成员</small><div>' +
          inheritedMembers.map(m => '<span class="tag">' + esc(m) + '</span>').join(' ') +
          '</div></div>';
      }
      el.innerHTML = html;
      el.querySelectorAll('[data-rm-member]').forEach(btn => {
        btn.addEventListener('click', () => {
          const username = btn.dataset.rmMember;
          if (pendingRemoves.has(username)) pendingRemoves.delete(username);
          else pendingRemoves.add(username);
          renderMembers();
          renderCandidates();
          updateStagingInfo();
        });
      });
      updateStagingInfo();
    }

    async function loadMembers() {
      const params = new URLSearchParams({
        name: groupName,
        page: String(memberPage),
        page_size: String(memberPageSize),
      });
      if (memberSearch) params.set('search', memberSearch);
      const detail = await Api.get('/api/admin/groups/members?' + params.toString());
      members = (detail.success ? detail.members : []) || [];
      inheritedMembers = (detail.success ? detail.inherited_members : []) || [];
      memberPage = detail.page || memberPage;
      memberTotalPages = detail.total_pages || 1;
      const info = overlay.querySelector('#gd-members-info');
      if (info) info.textContent = '第 ' + memberPage + ' / ' + memberTotalPages + ' 页，共 ' + (detail.total || 0) + ' 个直接成员';
      overlay.querySelector('#gd-members-prev').disabled = memberPage <= 1;
      overlay.querySelector('#gd-members-next').disabled = memberPage >= memberTotalPages;
      pendingAdds.clear();
      pendingRemoves.clear();
      renderMembers();
      renderCandidates();
    }

    async function loadCandidates() {
      if (unifiedAuth) return;
      const el = overlay.querySelector('#gd-candidates');
      if (!el) return;
      el.innerHTML = '<small class="text-muted">加载中...</small>';
      const params = new URLSearchParams({
        runtime: 'false',
        page: String(candidatePage),
        page_size: '20',
      });
      if (candidateSearch) params.set('search', candidateSearch);
      const data = await Api.get('/api/admin/users?' + params.toString());
      candidateExisting = new Set(members);
      candidateUsers = (data.success ? (data.users || []) : []).filter(u => u.role === 'user' && u.source === 'local');
      candidatePage = data.page || candidatePage;
      candidateTotalPages = data.total_pages || 1;
      const info = overlay.querySelector('#gd-candidates-info');
      if (info) info.textContent = '第 ' + candidatePage + ' / ' + candidateTotalPages + ' 页，共 ' + (data.total || 0) + ' 个用户';
      overlay.querySelector('#gd-candidates-prev').disabled = candidatePage <= 1;
      overlay.querySelector('#gd-candidates-next').disabled = candidatePage >= candidateTotalPages;
      renderCandidates();
    }

    function renderCandidates() {
      const el = overlay.querySelector('#gd-candidates');
      if (!el) return;
      if (candidateUsers.length === 0) {
        el.innerHTML = '<small class="text-muted">未找到可添加用户</small>';
        return;
      }
      el.innerHTML = candidateUsers.map(u => {
        const isMember = candidateExisting.has(u.username);
        const isPendingRemove = pendingRemoves.has(u.username);
        const isPendingAdd = pendingAdds.has(u.username);
        if (isMember && !isPendingRemove) return '<span class="tag tag-muted">' + esc(u.username) + ' <small>已加入</small></span>';
        if (isPendingAdd) return '<span class="tag" style="background:#e0f2fe;border-color:#7dd3fc">' + esc(u.username) + ' <small style="color:#0369a1">待加入</small> <button data-undo-add="' + esc(u.username) + '" style="color:#dc2626">&times;</button></span>';
        return '<span class="tag">' + esc(u.username) + ' <button data-add-member="' + esc(u.username) + '">+</button></span>';
      }).join(' ');
      el.querySelectorAll('[data-add-member]').forEach(btn => {
        btn.addEventListener('click', () => {
          const username = btn.dataset.addMember;
          pendingAdds.add(username);
          if (pendingRemoves.has(username)) pendingRemoves.delete(username);
          renderMembers();
          renderCandidates();
          updateStagingInfo();
        });
      });
      el.querySelectorAll('[data-undo-add]').forEach(btn => {
        btn.addEventListener('click', () => {
          pendingAdds.delete(btn.dataset.undoAdd);
          renderMembers();
          renderCandidates();
          updateStagingInfo();
        });
      });
    }

    await loadMembers();
    await loadCandidates();

    overlay.querySelector('#gd-save-btn')?.addEventListener('click', async () => {
      if (saving) return;
      saving = true;
      const saveBtn = overlay.querySelector('#gd-save-btn');
      saveBtn.disabled = true;
      saveBtn.textContent = '保存中...';
      try {
        const toAdd = Array.from(pendingAdds);
        const toRemove = Array.from(pendingRemoves);
        if (toRemove.length > 0) {
          for (const username of toRemove) {
            const res = await Api.post('/api/admin/groups/members/remove', { group_name: groupName, username });
            if (!res.success) { await alertModal('移除 ' + username + ' 失败: ' + (res.error || '未知错误')); saving = false; saveBtn.textContent = '保存'; saveBtn.disabled = false; return; }
          }
        }
        if (toAdd.length > 0) {
          const res = await Api.post('/api/admin/groups/members/add', { group_name: groupName, usernames: toAdd.length === 1 ? toAdd[0] : toAdd });
          if (!res.success) { await alertModal('添加成员失败: ' + (res.error || '未知错误')); saving = false; saveBtn.textContent = '保存'; saveBtn.disabled = false; return; }
        }
        loadGroups();
        overlay.remove();
      } catch (e) {
        await alertModal('保存失败: ' + e.message);
        saving = false;
        saveBtn.textContent = '保存';
        saveBtn.disabled = false;
      }
    });

    overlay.querySelector('#gd-cancel-btn')?.addEventListener('click', () => {
      overlay.remove();
    });

    overlay.querySelector('#gd-member-search-btn')?.addEventListener('click', async () => {
      memberSearch = overlay.querySelector('#gd-member-search').value.trim();
      memberPage = 1;
      await loadMembers();
    });
    overlay.querySelector('#gd-member-search')?.addEventListener('keydown', async e => {
      if (e.key === 'Enter') {
        memberSearch = e.target.value.trim();
        memberPage = 1;
        await loadMembers();
      }
    });
    overlay.querySelector('#gd-members-prev')?.addEventListener('click', async () => {
      if (memberPage > 1) {
        memberPage--;
        await loadMembers();
      }
    });
    overlay.querySelector('#gd-members-next')?.addEventListener('click', async () => {
      if (memberPage < memberTotalPages) {
        memberPage++;
        await loadMembers();
      }
    });
    overlay.querySelector('#gd-candidate-search-btn')?.addEventListener('click', async () => {
      candidateSearch = overlay.querySelector('#gd-candidate-search').value.trim();
      candidatePage = 1;
      await loadCandidates();
    });
    overlay.querySelector('#gd-candidate-search')?.addEventListener('keydown', async e => {
      if (e.key === 'Enter') {
        candidateSearch = e.target.value.trim();
        candidatePage = 1;
        await loadCandidates();
      }
    });
    overlay.querySelector('#gd-candidates-prev')?.addEventListener('click', async () => {
      if (candidatePage > 1) {
        candidatePage--;
        await loadCandidates();
      }
    });
    overlay.querySelector('#gd-candidates-next')?.addEventListener('click', async () => {
      if (candidatePage < candidateTotalPages) {
        candidatePage++;
        await loadCandidates();
      }
    });
  }

  function openCreateGroupModal() {
    ctx.$('#group-create-modal')?.remove();
    const overlay = document.createElement('div');
    overlay.id = 'group-create-modal';
    overlay.className = 'modal-overlay';
    const parentOptions = ['<option value="">顶级组</option>'].concat(allGroups.map(g => '<option value="' + g.id + '">' + esc(g.name) + '</option>')).join('');
    overlay.innerHTML =
      '<div class="modal">' +
        '<div class="modal-header">新建用户组<button id="modal-close">&times;</button></div>' +
        '<div class="modal-body">' +
          '<div class="field"><label>组名</label><input type="text" id="group-create-name" placeholder="dev-team"></div>' +
          '<div class="field"><label>父组</label><select id="group-create-parent">' + parentOptions + '</select></div>' +
          '<div class="field"><label>描述</label><input type="text" id="group-create-desc" placeholder="可选"></div>' +
          '<div id="group-create-msg" class="msg"></div>' +
        '</div>' +
        '<div class="modal-footer"><button class="btn btn-primary" id="group-create-submit">创建</button></div>' +
      '</div>';
    ctx.$('#content-area').appendChild(overlay);
    overlay.querySelector('#modal-close').addEventListener('click', () => overlay.remove());
    overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });
    overlay.querySelector('#group-create-submit').addEventListener('click', async () => {
      const name = overlay.querySelector('#group-create-name').value.trim();
      if (!name) { showMsg('#group-create-msg', '请输入组名', false); return; }
      const parentID = overlay.querySelector('#group-create-parent').value;
      const description = overlay.querySelector('#group-create-desc').value.trim();
      const params = { name, description };
      if (parentID) params.parent_id = parentID;
      const res = await Api.post('/api/admin/groups/create', params);
      if (res.success) {
        overlay.remove();
        showMsg('#groups-msg', res.message, true);
        loadGroups();
      } else {
        showMsg('#group-create-msg', res.error || '创建失败', false);
      }
    });
  }

  function pollGroupsTask(selector, initialMsg) {
    showMsg(selector, initialMsg + '...', true);
    var poll = async function() {
      try {
        var st = await Api.get('/api/admin/task/status');
        if (st.running) {
          var pct = st.total > 0 ? Math.round((st.done + st.failed) / st.total * 100) : 0;
          showMsg(selector, initialMsg + ' ' + (st.done + st.failed) + '/' + st.total + ' (' + pct + '%)', true);
          setTimeout(poll, 2000);
        } else {
          showMsg(selector, st.message || '完成', st.failed === 0);
        }
      } catch (e) {
        showMsg(selector, '查询进度失败: ' + e.message, false);
      }
    };
    setTimeout(poll, 1500);
  }
}
