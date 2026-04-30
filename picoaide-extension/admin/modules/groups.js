export async function init(ctx) {
  const { Api, esc, showMsg } = ctx;

  let currentGroups = [];

  ctx.$('#refresh-groups').addEventListener('click', loadGroups);
  ctx.$('#create-group-btn').addEventListener('click', () => openCreateGroupModal(currentGroups));

  // 统一认证模式下隐藏新建组按钮
  try {
    const cfgData = await Api.get('/api/config');
    const isUnified = cfgData.web?.auth_mode === 'ldap';
    if (isUnified) {
      ctx.$('#create-group-btn').classList.add('hidden');
    }
  } catch {}

  await loadGroups();

  async function loadGroups() {
    const tbody = ctx.$('#groups-tbody');
    tbody.innerHTML = '<tr><td colspan="5" class="text-center">加载中...</td></tr>';
    ctx.$('#groups-empty').classList.add('hidden');

    const data = await Api.get('/api/admin/groups');
    if (!data.success) { tbody.innerHTML = ''; return; }
    const groups = data.groups || [];
    currentGroups = groups;

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
        tr.innerHTML = '<td><strong>' + indent + esc(g.name) + '</strong>' + subInfo + '</td><td>' + srcBadge + '</td><td>' + g.member_count + '</td><td>' + g.skill_count + '</td><td><div class="btn-group"><button class="btn btn-sm btn-outline" data-group-detail="' + esc(g.name) + '">详情</button><button class="btn btn-sm btn-outline" data-group-apply="' + esc(g.name) + '">下发配置</button><button class="btn btn-sm btn-danger" data-group-del="' + esc(g.name) + '">删除</button></div></td>';
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
        if (!confirm('确定要下发配置到组 ' + btn.dataset.groupApply + ' 的所有成员（含子组）并重启容器吗？')) return;
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
    tbody.querySelectorAll('[data-group-del]').forEach(btn => {
      btn.addEventListener('click', async () => {
        if (!confirm('删除组 ' + btn.dataset.groupDel + '？')) return;
        const res = await Api.post('/api/admin/groups/delete', { name: btn.dataset.groupDel });
        showMsg('#groups-msg', res.message || res.error, res.success);
        if (res.success) loadGroups();
      });
    });
  }

  function openCreateGroupModal(currentGroups) {
    ctx.$('#group-modal')?.remove();
    const overlay = document.createElement('div');
    overlay.id = 'group-modal';
    overlay.className = 'modal-overlay';

    let parentOptions = '<option value="">无（顶级组）</option>';
    if (currentGroups && currentGroups.length > 0) {
      const byParent = {};
      for (const g of currentGroups) {
        const pid = g.parent_id || 'root';
        if (!byParent[pid]) byParent[pid] = [];
        byParent[pid].push(g);
      }
      function addOpts(parentId, depth) {
        const children = byParent[parentId] || [];
        for (const g of children) {
          parentOptions += '<option value="' + g.id + '">' + '\u00A0\u00A0'.repeat(depth) + (depth > 0 ? '└ ' : '') + esc(g.name) + '</option>';
          addOpts(g.id, depth + 1);
        }
      }
      addOpts('root', 0);
    }

    overlay.innerHTML =
      '<div class="modal">' +
        '<div class="modal-header">新建用户组<button id="modal-close">&times;</button></div>' +
        '<div class="modal-body">' +
          '<div class="field"><label>组名</label><input type="text" id="group-name" placeholder="developers"></div>' +
          '<div class="field"><label>父组</label><select id="group-parent">' + parentOptions + '</select></div>' +
          '<div class="field"><label>描述</label><input type="text" id="group-desc" placeholder="开发团队"></div>' +
        '</div>' +
        '<div class="modal-footer"><button class="btn btn-primary" id="group-create-btn">创建</button></div>' +
      '</div>';
    ctx.$('#content-area').appendChild(overlay);
    overlay.querySelector('#modal-close').addEventListener('click', () => overlay.remove());
    overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });
    overlay.querySelector('#group-create-btn').addEventListener('click', async () => {
      const name = ctx.$('#group-name').value.trim();
      const desc = ctx.$('#group-desc').value.trim();
      const parentId = ctx.$('#group-parent').value;
      if (!name) { alert('请输入组名'); return; }
      const params = { name, description: desc };
      if (parentId) params.parent_id = parentId;
      const res = await Api.post('/api/admin/groups/create', params);
      if (res.success) { overlay.remove(); loadGroups(); }
      else alert(res.error);
    });
  }

  async function openGroupDetail(groupName) {
    ctx.$('#group-modal')?.remove();
    const overlay = document.createElement('div');
    overlay.id = 'group-modal';
    overlay.className = 'modal-overlay';
    overlay.innerHTML =
      '<div class="modal" style="max-width:600px">' +
        '<div class="modal-header">组: ' + esc(groupName) + '<button id="modal-close">&times;</button></div>' +
        '<div class="modal-body">' +
          '<h4>成员</h4>' +
          '<div id="gd-members" class="mb-1"></div>' +
          '<div class="row mb-1">' +
            '<input type="text" id="gd-add-member" placeholder="用户名，多个用逗号分隔" style="flex:1">' +
            '<button class="btn btn-sm btn-primary" id="gd-add-btn">添加</button>' +
          '</div>' +
          '<h4>绑定技能</h4>' +
          '<div id="gd-skills" class="mb-1"></div>' +
          '<div class="row mb-1">' +
            '<select id="gd-skill-select" style="flex:1"></select>' +
            '<button class="btn btn-sm btn-primary" id="gd-bind-btn">绑定并部署</button>' +
          '</div>' +
        '</div>' +
      '</div>';
    ctx.$('#content-area').appendChild(overlay);
    overlay.querySelector('#modal-close').addEventListener('click', () => overlay.remove());
    overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });

    const detail = await Api.get('/api/admin/groups/members?name=' + encodeURIComponent(groupName));
    const members = (detail.success ? detail.members : []) || [];
    const skills = (detail.success ? detail.skills : []) || [];

    const skillData = await Api.get('/api/admin/skills');
    const allSkills = (skillData.success ? skillData.skills : []) || [];
    const skillSel = overlay.querySelector('#gd-skill-select');
    skillSel.innerHTML = '<option value="">选择技能</option>';
    for (const sk of allSkills) {
      skillSel.innerHTML += '<option value="' + esc(sk.name) + '">' + esc(sk.name) + '</option>';
    }

    function renderMembers() {
      const el = overlay.querySelector('#gd-members');
      if (members.length === 0) { el.innerHTML = '<small class="text-muted">暂无成员</small>'; return; }
      el.innerHTML = members.map(m => '<span class="tag">' + esc(m) + ' <button data-rm-member="' + esc(m) + '">&times;</button></span>').join(' ');
      el.querySelectorAll('[data-rm-member]').forEach(btn => {
        btn.addEventListener('click', async () => {
          const res = await Api.post('/api/admin/groups/members/remove', { group_name: groupName, username: btn.dataset.rmMember });
          if (res.success) { members.splice(members.indexOf(btn.dataset.rmMember), 1); renderMembers(); }
          else alert(res.error);
        });
      });
    }

    function renderSkills() {
      const el = overlay.querySelector('#gd-skills');
      if (skills.length === 0) { el.innerHTML = '<small class="text-muted">暂无绑定技能</small>'; return; }
      el.innerHTML = skills.map(s => '<span class="tag">' + esc(s) + ' <button data-rm-skill="' + esc(s) + '">&times;</button></span>').join(' ');
      el.querySelectorAll('[data-rm-skill]').forEach(btn => {
        btn.addEventListener('click', async () => {
          const res = await Api.post('/api/admin/groups/skills/unbind', { group_name: groupName, skill_name: btn.dataset.rmSkill });
          if (res.success) { skills.splice(skills.indexOf(btn.dataset.rmSkill), 1); renderSkills(); loadGroups(); }
          else alert(res.error);
        });
      });
    }

    renderMembers();
    renderSkills();

    overlay.querySelector('#gd-add-btn').addEventListener('click', async () => {
      const input = overlay.querySelector('#gd-add-member');
      const names = input.value.split(',').map(s => s.trim()).filter(Boolean);
      if (names.length === 0) return;
      const res = await Api.post('/api/admin/groups/members/add', { group_name: groupName, usernames: names.join(',') });
      if (res.success) { input.value = ''; names.forEach(n => { if (!members.includes(n)) members.push(n); }); renderMembers(); loadGroups(); }
      else alert(res.error);
    });

    overlay.querySelector('#gd-bind-btn').addEventListener('click', async () => {
      const skillName = skillSel.value;
      if (!skillName) { alert('请选择技能'); return; }
      const res = await Api.post('/api/admin/groups/skills/bind', { group_name: groupName, skill_name: skillName });
      if (res.success) { if (!skills.includes(skillName)) skills.push(skillName); renderSkills(); loadGroups(); alert(res.message); }
      else alert(res.error);
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
