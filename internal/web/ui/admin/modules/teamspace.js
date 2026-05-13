export async function init(ctx) {
  var $ = ctx.$, esc = ctx.esc, showMsg = ctx.showMsg, Api = ctx.Api;
  var allFolders = [], allGroups = [], currentId = 0;

  // ====== 标签选择器 ======
  function initTagSel(prefix) {
    var input = document.getElementById(prefix + '-input');
    var dd = document.getElementById(prefix + '-dropdown');
    var wrap = document.getElementById(prefix + '-wrap');
    var arrow = document.getElementById(prefix + '-arrow');
    if (!input) return;

    function toggleDrop(force) {
      var open = force !== undefined ? force : dd.classList.contains('open');
      if (open) { dd.classList.remove('open'); if (arrow) arrow.classList.remove('open'); }
      else { render(); dd.classList.add('open'); if (arrow) arrow.classList.add('open'); input.focus(); }
    }

    if (arrow) arrow.addEventListener('mousedown', function(e) { e.preventDefault(); toggleDrop(); });
    if (wrap) wrap.addEventListener('mousedown', function(e) { e.preventDefault(); input.focus(); });
    input.addEventListener('focus', function() { if (!dd.classList.contains('open')) toggleDrop(true); });
    input.addEventListener('blur', function() { toggleDrop(false); });
    input.addEventListener('input', render);
    input.addEventListener('keydown', function(e) {
      if (e.key === 'Escape') { toggleDrop(false); input.blur(); return; }
      if (e.key === 'Enter') {
        e.preventDefault(); var hl = dd.querySelector('.highlighted');
        if (hl) { toggleItem(hl); render(); }
        return;
      }
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        var items = dd.querySelectorAll('.sf-tag-option:not(.disabled)');
        if (!items.length) return;
        var cur = dd.querySelector('.highlighted'), idx = -1;
        for (var i = 0; i < items.length; i++) { if (items[i] === cur) { idx = i; break; } }
        var nxt = e.key === 'ArrowDown' ? idx + 1 : idx - 1;
        if (nxt < 0) nxt = items.length - 1; if (nxt >= items.length) nxt = 0;
        if (cur) cur.classList.remove('highlighted'); items[nxt].classList.add('highlighted');
      }
    });

    function toggleItem(el) {
      var ids = JSON.parse(input.dataset.selected || '[]');
      var id = Number(el.dataset.id), name = el.dataset.name;
      var idx = ids.findIndex(function(x) { return x.id === id; });
      if (idx >= 0) ids.splice(idx, 1);
      else ids.push({id: id, name: name});
      input.dataset.selected = JSON.stringify(ids);
      renderChips();
    }

    function render() {
      var groups = allGroups;
      var q = input.value.trim().toLowerCase();
      if (q) groups = allGroups.filter(function(g) { return g.name.toLowerCase().indexOf(q) !== -1; });
      var sel = {}; JSON.parse(input.dataset.selected || '[]').forEach(function(x) { sel[x.id] = true; });
      if (!groups.length) { dd.innerHTML = '<div class="sf-tag-option disabled">无匹配组</div>'; dd.classList.add('open'); if (arrow) arrow.classList.add('open'); return; }
      dd.innerHTML = groups.map(function(g) {
        var checked = sel[g.id];
        return '<div class="sf-tag-option' + (checked ? ' highlighted' : '') + '" data-id="' + g.id + '" data-name="' + esc(g.name) + '">' +
          '<span class="sf-tag-opt-check' + (checked ? ' checked' : '') + '"></span>' +
          esc(g.name) + '</div>';
      }).join('');
      [].forEach.call(dd.querySelectorAll('.sf-tag-option'), function(el) {
        el.addEventListener('mousedown', function(e) { e.preventDefault(); toggleItem(el); render(); });
      });
      dd.classList.add('open'); if (arrow) arrow.classList.add('open');
    }

    function renderChips() {
      var chips = document.getElementById(prefix + '-chips');
      var ids = JSON.parse(input.dataset.selected || '[]');
      if (!ids.length) { chips.innerHTML = '<span class="text-sm text-muted">未选择</span>'; return; }
      chips.innerHTML = ids.map(function(x) { return '<span class="chip">' + esc(x.name) + '<button type="button" class="chip-remove" data-id="' + x.id + '">&times;</button></span>'; }).join('');
      [].forEach.call(chips.querySelectorAll('.chip-remove'), function(btn) {
        btn.addEventListener('click', function(e) {
          e.stopPropagation();
          var xid = Number(btn.dataset.id);
          var cur = JSON.parse(input.dataset.selected || '[]');
          input.dataset.selected = JSON.stringify(cur.filter(function(c) { return c.id !== xid; }));
          renderChips();
        });
      });
    }
  }

  function selIds(prefix) {
    var input = document.getElementById(prefix + '-input');
    return input ? JSON.parse(input.dataset.selected || '[]').map(function(x) { return String(x.id); }) : [];
  }

  function setSel(prefix, items) {
    var input = document.getElementById(prefix + '-input');
    if (!input) return;
    input.dataset.selected = JSON.stringify(items.map(function(x) { return {id: x.id, name: x.name}; }));
    var chips = document.getElementById(prefix + '-chips');
    if (!items.length) { chips.innerHTML = '<span class="text-sm text-muted">未选择</span>'; return; }
    chips.innerHTML = items.map(function(x) { return '<span class="chip">' + esc(x.name) + '<button type="button" class="chip-remove" data-id="' + x.id + '">&times;</button></span>'; }).join('');
    [].forEach.call(chips.querySelectorAll('.chip-remove'), function(btn) {
      btn.addEventListener('click', function(e) {
        e.stopPropagation();
        var xid = Number(btn.dataset.id);
        var cur = JSON.parse(input.dataset.selected || '[]');
        input.dataset.selected = JSON.stringify(cur.filter(function(c) { return c.id !== xid; }));
        var upd = document.getElementById(prefix + '-chips');
        var remain = JSON.parse(input.dataset.selected || '[]');
        if (!remain.length) { upd.innerHTML = '<span class="text-sm text-muted">未选择</span>'; return; }
        upd.innerHTML = remain.map(function(x) { return '<span class="chip">' + esc(x.name) + '<button type="button" class="chip-remove" data-id="' + x.id + '">&times;</button></span>'; }).join('');
      });
    });
  }

  initTagSel('sf-create');

  // ====== 事件绑定 ======
  $('#sf-create-btn').addEventListener('click', openCreate);
  $('#sf-create-cancel').addEventListener('click', function() { $('#sf-create-area').classList.add('hidden'); });
  $('#sf-create-save').addEventListener('click', handleCreate);
  $('#sf-btn-edit').addEventListener('click', openEditModal);
  $('#sf-btn-check').addEventListener('click', handleCheckMount);
  $('#sf-btn-mount').addEventListener('click', handleMountAll);
  $('#sf-btn-delete').addEventListener('click', handleDelete);

  await Promise.all([loadFolders(), loadGroups()]);

  // ====== 数据 ======
  async function loadFolders() {
    var data = await Api.get('/api/admin/shared-folders');
    if (!data.success) return;
    allFolders = data.folders || [];
    renderList();
    if (currentId) {
      var f = allFolders.find(function(x) { return x.id === currentId; });
      if (f) showDetail(f); else { currentId = 0; showPlaceholder(); }
    }
  }

  async function loadGroups() {
    var data = await Api.get('/api/admin/groups');
    if (!data.success) return;
    allGroups = data.groups || [];
  }

  // ====== 列表 ======
  function renderList() {
    var list = $('#sf-list');
    if (!allFolders.length) { list.innerHTML = '<p class="text-muted text-center" style="padding:1rem">暂无共享文件夹</p>'; return; }
    list.innerHTML = allFolders.map(function(f) {
      var badges = '';
      if (f.is_public) badges += '<span class="badge badge-ok">公共</span>';
      if (f.orphaned) badges += '<span class="badge badge-danger">孤立</span>';
      return '<button class="nav-item' + (f.id === currentId ? ' active' : '') + '" data-id="' + f.id + '">' +
        '<span class="nav-item-main"><span class="nav-item-title">' + esc(f.name) + '</span><span class="nav-item-subtitle">' + esc(f.member_count + ' 人') + '</span></span>' +
        '<span class="badge-group">' + badges + '</span></button>';
    }).join('');
    [].forEach.call(list.querySelectorAll('[data-id]'), function(btn) {
      btn.addEventListener('click', function() {
        currentId = Number(btn.dataset.id); renderList();
        var f = allFolders.find(function(x) { return x.id === currentId; });
        if (f) showDetail(f);
      });
    });
  }

  // ====== 详情 ======
  function showPlaceholder() { $('#sf-detail-placeholder').classList.remove('hidden'); $('#sf-detail').classList.add('hidden'); }

  function showDetail(f) {
    $('#sf-detail-placeholder').classList.add('hidden'); $('#sf-detail').classList.remove('hidden');
    var b = '';
    if (f.is_public) b += '<span class="badge badge-ok">公共</span>';
    if (f.orphaned) b += '<span class="badge badge-danger">⚠ 无关联组</span>';
    $('#sf-view-name').textContent = f.name; $('#sf-view-badges').innerHTML = b;
    $('#sf-view-desc').textContent = f.description || '(无描述)';
    $('#sf-stat-members').textContent = f.member_count;
    $('#sf-stat-mounted').textContent = f.mounted_count;
    $('#sf-stat-unmounted').textContent = f.member_count - f.mounted_count;
    if (f.groups && f.groups.length) {
      $('#sf-view-groups').innerHTML = f.groups.map(function(g) { return '<span class="chip">' + esc(g.name) + '</span>'; }).join('');
    } else if (f.is_public) {
      $('#sf-view-groups').innerHTML = '<span class="text-muted">公共共享，无需关联组</span>';
    } else {
      $('#sf-view-groups').innerHTML = '<span class="text-muted">无关联组</span>';
    }
    // 成员列表改为查看按钮
    var tb = $('#sf-member-tbody');
    var hasMembers = f.members && f.members.length > 0;
    tb.innerHTML = '<tr><td colspan="4" class="text-center">' +
      (hasMembers
        ? '<button class="btn btn-sm btn-outline" id="sf-btn-view-members">查看成员 (' + f.member_count + ' 人)</button>'
        : '<span class="text-muted">暂无成员</span>') +
      '</td></tr>';
    var viewBtn = document.getElementById('sf-btn-view-members');
    if (viewBtn) {
      viewBtn.addEventListener('click', function() { showMembersModal(f); });
    }
  }

  // ====== 成员弹窗 ======
  function showMembersModal(f) {
    var membersHtml = (!f.members || !f.members.length)
      ? '<p class="text-muted text-center">暂无成员</p>'
      : '<div class="table-wrap"><table class="compact-table"><thead><tr><th>用户名</th><th>挂载状态</th><th>最后检查</th><th>操作</th></tr></thead><tbody>' +
        f.members.map(function(m) {
          var c = m.mounted ? 'badge-ok' : 'badge-muted', t = m.mounted ? '✓ 已挂载' : '✗ 未挂载';
          return '<tr><td>' + esc(m.username) + '</td><td><span class="badge ' + c + '">' + t + '</span></td><td class="text-sm text-muted">' + (m.checked_at || '-') + '</td><td class="actions-cell"><button class="btn btn-sm btn-outline chk-single" data-u="' + esc(m.username) + '">检查</button></td></tr>';
        }).join('') +
        '</tbody></table></div>';

    showModal({ title: '成员列表 - ' + f.name, width: '600px', body: membersHtml, footer: [
      { label: '关闭', value: 'close' }
    ]}).catch(function() {});

    // 为弹窗内的检查按钮绑定事件
    setTimeout(function() {
      document.querySelectorAll('.chk-single').forEach(function(btn) {
        btn.addEventListener('click', async function() {
          await checkSingle(f.id, btn.dataset.u);
          // 刷新数据后更新弹窗内容
          var refreshed = await Api.get('/api/admin/shared-folders');
          if (refreshed.success) {
            var folder = (refreshed.folders || []).find(function(x) { return x.id === f.id; });
            if (folder) {
              // 关闭当前弹窗，重新打开
              document.querySelector('.modal-overlay')?.remove();
              showMembersModal(folder);
            }
          }
        });
      });
    }, 100);
  }

  // ====== 新建 ======
  function openCreate() {
    $('#sf-create-area').classList.remove('hidden');
    $('#sf-create-name').value = ''; $('#sf-create-desc').value = '';
    $('#sf-create-public').checked = false; $('#sf-create-msg').textContent = '';
    setSel('sf-create', []);
    $('#sf-create-area').scrollIntoView({ behavior: 'smooth' });
  }

  // ====== 编辑（模态框） ======
  async function openEditModal() {
    var f = allFolders.find(function(x) { return x.id === currentId; });
    if (!f) return;

    var body = '<div class="field"><label>名称 *</label><input type="text" id="sf-modal-name" value="' + esc(f.name) + '"></div>' +
      '<div class="field"><label>描述</label><textarea id="sf-modal-desc" rows="2">' + esc(f.description || '') + '</textarea></div>' +
      '<div class="field"><label>类型</label>' +
        '<label class="toggle-switch"><input type="checkbox" id="sf-modal-public"' + (f.is_public ? ' checked' : '') + '>' +
        '<span class="toggle-switch-control"></span><span class="toggle-switch-label">公共共享</span></label></div>' +
      '<div class="field"><label>关联用户组</label>' +
        '<div class="sf-tag-selector" id="sf-modal-tag">' +
          '<div class="sf-tag-chips" id="sf-modal-chips"></div>' +
          '<div class="sf-tag-input-wrap" id="sf-modal-wrap">' +
            '<input type="text" class="sf-tag-input" id="sf-modal-input" placeholder="搜索用户组...">' +
            '<span class="sf-tag-arrow" id="sf-modal-arrow">▾</span>' +
          '</div>' +
          '<div class="sf-tag-dropdown" id="sf-modal-dropdown"></div>' +
        '</div></div>' +
      '<div id="sf-modal-msg" class="msg"></div>' +
      '<div class="toolbar" style="margin-top:.75rem">' +
        '<button class="btn btn-primary" id="sf-modal-save-btn">保存修改</button>' +
        '<button class="btn btn-outline" id="sf-modal-cancel-btn">取消</button>' +
      '</div>';

    showModal({ title: '编辑 - ' + f.name, width: '520px', body: body });

    // 初始化标签选择器
    setTimeout(function() {
      initTagSel('sf-modal');
      setSel('sf-modal', f.groups || []);
    }, 50);

    async function handleSave() {
      var name = document.getElementById('sf-modal-name').value.trim();
      if (!name) { document.getElementById('sf-modal-msg').textContent = '名称不能为空';
        document.getElementById('sf-modal-msg').className = 'msg msg-err'; return; }
      var desc = document.getElementById('sf-modal-desc').value.trim();
      var isPublic = document.getElementById('sf-modal-public').checked;
      var groupIDs = selIds('sf-modal').join(',');

      var r1 = await Api.post('/api/admin/shared-folders/update', {
        id: String(currentId), name: name,
        description: desc, is_public: isPublic ? '1' : '0',
      });
      if (!r1.success) { showMsg('#sf-modal-msg', r1.error, false); return; }
      // 仅当组选择有变化时才调用 groups/set，避免不必要重启容器
      var oldIDs = (f.groups || []).map(function(g) { return String(g.id); }).sort().join(',');
      if (groupIDs !== oldIDs) {
        var r2 = await Api.post('/api/admin/shared-folders/groups/set', {
          folder_id: String(currentId), group_ids: groupIDs,
        });
        showMsg('#sf-view-msg', r2.message || '已保存', r2.success);
      } else {
        showMsg('#sf-view-msg', '已保存', true);
      }
      // 关闭弹窗
      var overlay = document.querySelector('.modal-overlay');
      if (overlay) overlay.remove();
      if (r2.success) { await loadFolders(); }
    }

    // 延迟绑定，确保元素已渲染
    setTimeout(function() {
      document.getElementById('sf-modal-save-btn').addEventListener('click', handleSave);
      document.getElementById('sf-modal-cancel-btn').addEventListener('click', function() {
        var overlay = document.querySelector('.modal-overlay');
        if (overlay) overlay.remove();
      });
    }, 50);
  }

  // ====== 操作 ======
  async function handleCreate() {
    var msg = $('#sf-create-msg'), name = $('#sf-create-name').value.trim();
    if (!name) { showMsg(msg, '名称不能为空', false); return; }
    var data = await Api.post('/api/admin/shared-folders/create', {
      name: name, description: $('#sf-create-desc').value.trim(),
      is_public: $('#sf-create-public').checked ? '1' : '0',
      group_ids: selIds('sf-create').join(','),
    });
    showMsg(msg, data.message || data.error, data.success);
    if (data.success) { $('#sf-create-area').classList.add('hidden'); currentId = 0; await loadFolders(); }
  }

  async function handleDelete() {
    try {
      await showModal({ title: '确认删除', body: '<p style="margin:0">确定删除此共享文件夹？<br>文件将归档到 archive/，已挂载用户的容器将重启。</p>', footer: [
        { label: '取消', value: 'cancel' }, { label: '删除', value: 'delete', danger: true }
      ]});
    } catch(e) { return; }
    var data = await Api.post('/api/admin/shared-folders/delete', { id: String(currentId) });
    showMsg('#sf-view-msg', data.message || data.error, data.success);
    if (data.success) { currentId = 0; await loadFolders(); showPlaceholder(); }
  }

  async function handleCheckMount() {
    showMsg('#sf-view-msg', '正在检查...', true);
    var f = allFolders.find(function(x) { return x.id === currentId; });
    if (!f || !f.members) return;
    var ok = 0, fail = 0;
    for (var m of f.members) {
      var data = await Api.post('/api/admin/shared-folders/test', { folder_id: String(currentId), username: m.username });
      if (data.mounted) ok++; else fail++;
    }
    showMsg('#sf-view-msg', '检查完成: ' + ok + ' 已挂载, ' + fail + ' 未挂载', true);
    await loadFolders();
  }

  async function checkSingle(folderId, username) {
    var data = await Api.post('/api/admin/shared-folders/test', { folder_id: String(folderId), username: username });
    if (data.success) await loadFolders();
  }

  async function handleMountAll() {
    try { await showModal({ title: '确认挂载', body: '<p style="margin:0">将为所有成员重建容器并挂载共享目录，确定？</p>', footer: [
      { label: '取消', value: 'cancel' }, { label: '确认挂载', value: 'mount', primary: true }
    ]}); } catch(e) { return; }
    var data = await Api.post('/api/admin/shared-folders/mount', { folder_id: String(currentId) });
    showMsg('#sf-view-msg', data.message || data.error, data.success);
  }
}
