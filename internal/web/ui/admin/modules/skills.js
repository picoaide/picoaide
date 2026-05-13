// ============================================================
// 技能管理模块
// ============================================================

export async function init(ctx) {
  var { Api, esc, showMsg, showModal, confirmModal, $, $$ } = ctx;

  // 自定义复选框样式
  var ckbStyle = document.createElement('style');
  ckbStyle.textContent = '.ckb-row{display:flex;align-items:center;padding:.35rem .45rem;cursor:pointer;border-radius:4px;font-size:.85rem;gap:.4rem;transition:background .12s}.ckb-row:hover{background:#f1f5f9}.ckb-row.checked{background:#eff6ff}.ckb-box{width:18px;height:18px;border:2px solid #cbd5e1;border-radius:4px;display:flex;align-items:center;justify-content:center;flex-shrink:0;transition:all .12s;font-size:12px;color:#fff}.ckb-row.checked .ckb-box{background:#2563eb;border-color:#2563eb}.ckb-row.checked .ckb-box::after{content:"✓";font-weight:700}.ckb-input{position:absolute;opacity:0;pointer-events:none;width:0;height:0}';
  document.head.appendChild(ckbStyle);

  var skillsPage = 1;
  var skillsPageSize = 50;
  var skillsTotalPages = 1;
  var skillsSearch = '';
  var skillsSourceFilter = '';

  await loadSources();
  await loadSkills();

  $('#add-git-source-btn').addEventListener('click', showAddGitSourceModal);
  $('#batch-deploy-btn').addEventListener('click', openBatchDeployWizard);
  $('#skills-search-btn').addEventListener('click', function() {
    skillsSearch = $('#skills-search').value.trim();
    skillsPage = 1;
    loadSkills();
  });
  $('#skills-search').addEventListener('keydown', function(e) {
    if (e.key === 'Enter') {
      skillsSearch = e.target.value.trim();
      skillsPage = 1;
      loadSkills();
    }
  });
  $('#skills-page-size').addEventListener('change', function(e) {
    skillsPageSize = Number(e.target.value) || 50;
    skillsPage = 1;
    loadSkills();
  });
  $('#skills-prev').addEventListener('click', function() {
    if (skillsPage > 1) { skillsPage--; loadSkills(); }
  });
  $('#skills-next').addEventListener('click', function() {
    if (skillsPage < skillsTotalPages) { skillsPage++; loadSkills(); }
  });

  // ====================================================================
  // 通用
  // ====================================================================

  function makeCkbRow(value, labelText, extraHtml) {
    var row = document.createElement('label');
    row.className = 'ckb-row';
    row.innerHTML = '<input type="checkbox" class="ckb-input" value="' + esc(value) + '"><span class="ckb-box"></span><span>' + esc(labelText) + '</span>' + (extraHtml || '');
    row.querySelector('.ckb-input').addEventListener('change', function() {
      row.classList.toggle('checked', this.checked);
    });
    return row;
  }

  function findSkillSource(skillName) {
    var d = window._lastSkillsData; if (!d) return '';
    var sk = (d.skills || []).find(function(s) { return s.name === skillName; });
    return sk ? (sk.source || '') : '';
  }

  async function getCSRF() {
    try { var r = await fetch('/api/csrf', { credentials: 'include' }); var d = await r.json(); return d.csrf_token || ''; } catch(e) { return ''; }
  }

  // ====================================================================
  // 源管理
  // ====================================================================

  async function loadSources() {
    var list = $('#sources-list');
    list.innerHTML = '';
    var data = await Api.get('/api/admin/skills/sources');
    var sources = data.sources || [];
    if (sources.length === 0) return;

    // 源筛选按钮
    var filterHtml = '';
    if (sources.length > 1) {
      filterHtml = '<button class="btn btn-sm btn-outline source-filter-btn active" data-source="">全部</button>';
      for (var src of sources) {
        filterHtml += '<button class="btn btn-sm btn-outline source-filter-btn" data-source="' + esc(src.name) + '">' + esc(src.display_name || src.name) + '</button>';
      }
    }
    var filterEl = $('#skills-source-filter');
    if (filterEl) {
      filterEl.innerHTML = filterHtml;
      filterEl.querySelectorAll('.source-filter-btn').forEach(function(btn) {
        btn.addEventListener('click', function() {
          filterEl.querySelectorAll('.source-filter-btn').forEach(function(b) { b.classList.remove('active'); });
          btn.classList.add('active');
          skillsSourceFilter = btn.dataset.source || '';
          skillsPage = 1;
          loadSkills();
        });
      });
    }

    for (var src of sources) {
      var card = document.createElement('div');
      card.className = 'card';
      card.style.marginBottom = '.65rem';

      var isRegistry = src.type === 'registry';
      var typeLabel = isRegistry ? '注册源' : 'Git';
      var typeBadge = isRegistry ? 'badge badge-info' : 'badge';
      var nameDisplay = src.display_name || src.name;
      var actionsHtml = isRegistry
        ? '<button class="btn btn-sm btn-outline" data-browse="' + esc(src.name) + '">浏览技能</button><button class="btn btn-sm btn-outline" data-refresh="' + esc(src.name) + '">刷新索引</button>'
        : '<button class="btn btn-sm btn-outline" data-pull="' + esc(src.name) + '">拉取更新</button><button class="btn btn-sm btn-danger" data-rm-source="' + esc(src.name) + '">删除源</button>';

      card.innerHTML = '' +
        '<div class="card-header">' +
          '<div><div style="font-weight:700">' + esc(nameDisplay) + ' <span class="' + typeBadge + '">' + typeLabel + '</span></div>' +
          '<span class="text-muted text-sm">' + esc(src.url || '') + '</span></div>' +
          '<div class="btn-group">' + actionsHtml + '</div>' +
        '</div>' +
        '<div class="card-footer" style="padding:.5rem .85rem">' +
          '<span class="text-sm">技能数: <strong>' + (src.skill_count || 0) + '</strong></span>' +
          (src.last_pull ? '<span class="text-sm text-muted" style="margin-left:1rem">上次拉取: ' + esc(src.last_pull) + '</span>' : '') +
          (src.last_refresh ? '<span class="text-sm text-muted" style="margin-left:1rem">上次刷新: ' + esc(src.last_refresh) + '</span>' : '') +
        '</div>';
      list.appendChild(card);

      if (isRegistry) {
        card.querySelector('[data-browse]').addEventListener('click', function() { openRegistryBrowser(src); });
        card.querySelector('[data-refresh]').addEventListener('click', async function() {
          var btn = this; btn.disabled = true; btn.textContent = '刷新中...';
          var res = await Api.post('/api/admin/skills/sources/refresh', { name: src.name });
          showMsg('#skills-msg', res.message || res.error, res.success);
          btn.disabled = false; btn.textContent = '刷新索引';
          if (res.success) loadSources();
        });
      } else {
        card.querySelector('[data-pull]').addEventListener('click', async function() {
          var btn = this; btn.disabled = true; btn.textContent = '拉取中...';
          var res = await Api.post('/api/admin/skills/sources/pull', { name: src.name });
          showMsg('#skills-msg', res.message || res.error, res.success);
          btn.disabled = false; btn.textContent = '拉取更新';
          if (res.success) { loadSources(); loadSkills(); }
        });
        card.querySelector('[data-rm-source]').addEventListener('click', async function() {
          if (!await confirmModal('确定删除源 ' + esc(src.display_name || src.name) + '？所有关联技能将从用户解绑。')) return;
          var res = await Api.post('/api/admin/skills/sources/remove', { name: src.name });
          showMsg('#skills-msg', res.message || res.error, res.success);
          if (res.success) loadSources();
        });
      }
    }
  }

  // ====================================================================
  // 添加 Git 源（分步引导弹窗）
  // ====================================================================

  function showAddGitSourceModal() {
    var modal = $('#git-source-wizard');
    var step1 = $('#wizard-step-1');
    var step2 = $('#wizard-step-2');
    var step3 = $('#wizard-step-3');
    var footer = $('#wizard-footer');
    var nextBtn = $('#wizard-next');
    var cancelBtn = $('#wizard-cancel');
    var errEl = $('#wizard-error');
    var nameInput = $('#wizard-name');
    var urlInput = $('#wizard-url');
    var refInput = $('#wizard-ref');
    var refType = $('#wizard-ref-type');

    function reset() {
      step1.classList.remove('hidden'); step2.classList.add('hidden'); step3.classList.add('hidden');
      footer.classList.remove('hidden'); nextBtn.textContent = '下一步'; nextBtn.classList.remove('hidden');
      cancelBtn.textContent = '取消'; cancelBtn.classList.remove('hidden');
      errEl.classList.add('hidden'); $('#wizard-log').textContent = ''; $('#wizard-status').textContent = '准备中';
    }

    reset(); modal.style.display = 'flex';
    function closeM() { modal.style.display = 'none'; reset(); }
    $('#wizard-close').onclick = closeM; cancelBtn.onclick = closeM;
    modal.onclick = function(e) { if (e.target === modal) closeM(); };

    nextBtn.onclick = async function() {
      if (nextBtn.textContent === '完成' || nextBtn.textContent === '关闭') { closeM(); return; }
      var name = nameInput.value.trim(); var url = urlInput.value.trim();
      if (!name) { errEl.textContent = '请输入源名称'; errEl.classList.remove('hidden'); return; }
      if (!url) { errEl.textContent = '请输入仓库 URL'; errEl.classList.remove('hidden'); return; }
      errEl.classList.add('hidden');

      step1.classList.add('hidden'); step2.classList.remove('hidden');
      nextBtn.disabled = true; nextBtn.textContent = '克隆中...'; cancelBtn.textContent = '后台运行';
      var logEl = $('#wizard-log'); var statusEl = $('#wizard-status');

      logEl.textContent += '📦 克隆 ' + url + ' ...\n'; statusEl.textContent = '正在克隆仓库...';
      try {
        var csrf = await getCSRF();
        var res = await Api.post('/api/admin/skills/sources/git', { name: name, url: url, ref: refInput.value.trim(), ref_type: refType.value, csrf_token: csrf });
        if (res.success) {
          logEl.textContent += '✅ ' + (res.message || '') + '\n'; statusEl.textContent = '✅ 添加成功';
          step2.classList.add('hidden'); step3.classList.remove('hidden');
          var skills = res.skills || [];
          $('#wizard-done-title').textContent = '添加成功';
          $('#wizard-done-desc').textContent = '发现 ' + skills.length + ' 个技能：' + (skills.join('、') || '');
          nextBtn.textContent = '完成'; nextBtn.disabled = false; cancelBtn.textContent = '关闭';
          loadSources(); loadSkills();
        } else {
          statusEl.textContent = '❌ 失败'; logEl.textContent += '❌ ' + (res.error || '') + '\n';
          step2.classList.add('hidden'); step1.classList.remove('hidden');
          nextBtn.disabled = false; nextBtn.textContent = '重试';
          errEl.textContent = res.error || '添加失败'; errEl.classList.remove('hidden');
        }
      } catch(e) {
        logEl.textContent += '❌ ' + e.message + '\n'; statusEl.textContent = '❌ 连接失败';
        step2.classList.add('hidden'); step1.classList.remove('hidden');
        nextBtn.disabled = false; nextBtn.textContent = '重试';
        errEl.textContent = e.message; errEl.classList.remove('hidden');
      }
    };
  }

  // ====================================================================
  // 注册源浏览器
  // ====================================================================

  async function openRegistryBrowser(src) {
    var overlay = $('#registry-modal');
    var list = $('#registry-list');
    overlay.classList.remove('hidden'); overlay.style.display = 'flex';
    $('#registry-modal-header').innerHTML = '浏览 ' + esc(src.display_name || src.name) + ' <button class="modal-close-btn" id="registry-modal-close">&times;</button>';

    async function loadRegistry(query) {
      list.innerHTML = '<p class="text-muted text-center" style="padding:2rem">加载中...</p>';
      var data = await Api.get('/api/admin/skills/registry/list?source=' + encodeURIComponent(src.name) + (query ? '&q=' + encodeURIComponent(query) : ''));
      var skills = data.skills || [];
      $('#registry-status').textContent = skills.length + ' 个技能';
      if (skills.length === 0) { list.innerHTML = '<p class="text-muted text-center" style="padding:2rem">未找到技能</p>'; return; }

      var installedData = await Api.get('/api/admin/skills');
      var installed = {}; (installedData.skills || []).forEach(function(s) { if (s.source === src.name) installed[s.name] = true; });

      list.innerHTML = '';
      for (var sk of skills) {
        var slug = sk.slug;
        var isInstalled = installed[slug] || false;
        var tags = (sk.categories || []).map(function(c) { return '<span class="badge badge-muted">' + esc(c) + '</span>'; }).join(' ');
        var card = document.createElement('div');
        card.className = 'card'; card.style.marginBottom = '.5rem';
        card.innerHTML = '' +
          '<div class="card-header" style="align-items:flex-start">' +
            '<div style="flex:1">' +
              '<div style="font-weight:700">' + esc(sk.name) + (sk.version ? ' <span class="badge badge-muted" style="font-size:.7rem;vertical-align:middle">' + esc(sk.version) + '</span>' : '') + '</div>' +
              '<div class="text-sm text-muted" style="margin-top:.2rem">' + esc((sk.description || '').substring(0, 200)) + '</div>' +
              (tags ? '<div style="margin-top:.3rem">' + tags + '</div>' : '') +
              '<div class="text-xs text-muted" style="margin-top:.2rem">slug: ' + esc(slug) + (sk.downloads ? ' · 下载: ' + sk.downloads : '') + (sk.stars ? ' · ⭐ ' + sk.stars : '') + '</div>' +
            '</div>' +
            '<div>' + (isInstalled ? '<span class="badge badge-success">已安装</span>' : '<button class="btn btn-sm btn-primary" data-install-slug="' + esc(slug) + '">安装</button>') + '</div>' +
          '</div>';
        list.appendChild(card);
      }

      list.querySelectorAll('[data-install-slug]').forEach(function(btn) {
        btn.addEventListener('click', async function() {
          var slug = btn.dataset.installSlug; btn.disabled = true; btn.textContent = '安装中...';
          var csrf = await getCSRF();
          var res = await Api.post('/api/admin/skills/registry/install', { source: src.name, slug: slug, csrf_token: csrf });
          showMsg('#skills-msg', res.message || res.error, res.success);
          if (res.success) { btn.textContent = '✓ 已安装'; btn.classList.remove('btn-primary'); btn.classList.add('badge', 'badge-success'); loadSkills(); }
          else { btn.disabled = false; btn.textContent = '安装'; }
        });
      });
    }

    await loadRegistry('');
    $('#registry-search').value = '';
    $('#registry-search-btn').onclick = function() { loadRegistry($('#registry-search').value.trim()); };
    $('#registry-search').onkeydown = function(e) { if (e.key === 'Enter') loadRegistry(e.target.value.trim()); };

    function closeM() { overlay.classList.add('hidden'); overlay.style.display = 'none'; }
    $('#registry-modal-close').onclick = closeM;
    $('#registry-modal-close-btn').onclick = closeM;
  }

  // ====================================================================
  // 技能列表
  // ====================================================================

  async function loadSkills() {
    var tbody = $('#skills-tbody');
    var empty = $('#skills-empty');
    tbody.innerHTML = ''; empty.classList.add('hidden');
    var params = new URLSearchParams({ page: String(skillsPage), page_size: String(skillsPageSize) });
    if (skillsSearch) params.set('search', skillsSearch);
    if (skillsSourceFilter) params.set('source', skillsSourceFilter);
    var data = await Api.get('/api/admin/skills?' + params.toString());
    if (!data.success) return;
    window._lastSkillsData = data;
    skillsPage = data.page || 1; skillsPageSize = data.page_size || 50; skillsTotalPages = data.total_pages || 1;
    updatePager(data);

    var skills = data.skills || [];
    if (skills.length === 0) { empty.classList.remove('hidden'); return; }

    for (var sk of skills) {
      var tr = document.createElement('tr');
      tr.innerHTML = '' +
        '<td><strong>' + esc(sk.name) + '</strong>' + (sk.description ? '<br><small style="color:var(--text-muted);font-size:.78rem">' + esc(sk.description) + '</small>' : '') + '</td>' +
        '<td>' + (sk.source ? '<span class="badge badge-muted">' + esc(sk.source) + '</span>' : '') + '</td>' +
        '<td>' + sk.file_count + ' 文件</td>' +
        '<td>' + sk.size_str + '</td>' +
        '<td>' + sk.mod_time + '</td>' +
        '<td class="actions-cell"><div class="btn-group">' +
          '<button class="btn btn-sm btn-outline" data-deploy="' + esc(sk.name) + '">部署</button>' +
          '<button class="btn btn-sm btn-outline" data-detail="' + esc(sk.name) + '">详情</button>' +
          '<button class="btn btn-sm btn-outline" data-dl="' + esc(sk.name) + '">下载</button>' +
          '<button class="btn btn-sm btn-danger" data-rm="' + esc(sk.name) + '">删除</button>' +
        '</div></td>';
      tbody.appendChild(tr);
    }

    tbody.querySelectorAll('[data-dl]').forEach(function(btn) {
      btn.addEventListener('click', function() { window.open(window.location.origin + '/api/admin/skills/download?name=' + encodeURIComponent(btn.dataset.dl), '_blank'); });
    });
    tbody.querySelectorAll('[data-rm]').forEach(function(btn) {
      btn.addEventListener('click', async function() {
        if (!await confirmModal('确定删除技能 ' + btn.dataset.rm + '？将从所有用户和组移除，不可撤销。')) return;
        var res = await Api.post('/api/admin/skills/remove', { name: btn.dataset.rm });
        showMsg('#skills-msg', res.message || res.error, res.success);
        if (res.success) loadSkills();
      });
    });
    tbody.querySelectorAll('[data-deploy]').forEach(function(btn) {
      btn.addEventListener('click', function() { openSingleDeployModal(btn.dataset.deploy); });
    });
    tbody.querySelectorAll('[data-detail]').forEach(function(btn) {
      btn.addEventListener('click', function() { openSkillDetail(btn.dataset.detail); });
    });
  }

  function updatePager(data) {
    var info = $('#skills-page-info');
    if (info) info.textContent = '第 ' + (data.page || 1) + ' / ' + (data.total_pages || 1) + ' 页，共 ' + (data.total || 0) + ' 个技能';
    $('#skills-page-size').value = String(skillsPageSize);
    $('#skills-prev').disabled = skillsPage <= 1;
    $('#skills-next').disabled = skillsPage >= skillsTotalPages;
  }

  // ====================================================================
  // 侧边详情面板
  // ====================================================================

  async function openSkillDetail(skillName) {
    var overlay = document.createElement('div');
    overlay.style.cssText = 'position:fixed;inset:0;background:rgba(15,23,42,.32);z-index:900';
    var panel = document.createElement('div');
    panel.style.cssText = 'position:fixed;top:0;right:0;width:520px;max-width:100vw;height:100vh;background:#fff;z-index:901;box-shadow:-8px 0 32px rgba(15,23,42,.12);display:flex;flex-direction:column;overflow:hidden';
    panel.innerHTML = '' +
      '<div style="display:flex;align-items:center;justify-content:space-between;padding:1rem 1.2rem;border-bottom:1px solid var(--border);font-weight:800;font-size:1rem">' +
        '<span>' + esc(skillName) + '</span>' +
        '<button class="close-btn" id="sp-close" style="background:none;border:none;font-size:1.4rem;color:var(--text-muted);cursor:pointer">&times;</button>' +
      '</div>' +
      '<div style="flex:1;overflow-y:auto;padding:1.2rem" id="sp-body">' +
        '<p id="sp-desc" class="text-muted" style="font-size:.85rem;margin-bottom:1rem"></p>' +
        '<div style="display:flex;gap:.28rem;border-bottom:1px solid var(--border);margin-bottom:1rem">' +
          '<button class="side-panel-tab active" data-sp-tab="users" style="padding:.5rem .85rem;background:none;border:none;border-bottom:2px solid var(--primary);margin-bottom:-1px;font-size:.85rem;font-weight:700;color:var(--primary);cursor:pointer">已绑定用户</button>' +
          '<button class="side-panel-tab" data-sp-tab="groups" style="padding:.5rem .85rem;background:none;border:none;border-bottom:2px solid transparent;margin-bottom:-1px;font-size:.85rem;font-weight:700;color:var(--text-muted);cursor:pointer">已绑定组</button>' +
          '<button class="side-panel-tab" data-sp-tab="source" style="padding:.5rem .85rem;background:none;border:none;border-bottom:2px solid transparent;margin-bottom:-1px;font-size:.85rem;font-weight:700;color:var(--text-muted);cursor:pointer">来源</button>' +
        '</div>' +
        '<div id="sp-users"></div>' +
        '<div id="sp-groups" style="display:none"></div>' +
        '<div id="sp-source" style="display:none"></div>' +
      '</div>' +
      '<div style="padding:.85rem 1.2rem;border-top:1px solid var(--border);display:flex;gap:.5rem;justify-content:flex-end">' +
        '<button class="btn btn-outline btn-sm" id="sp-bind-user">绑定用户</button>' +
        '<button class="btn btn-outline btn-sm btn-danger" id="sp-delete-skill" style="margin-left:auto">删除此技能</button>' +
      '</div>';
    document.body.appendChild(overlay);
    document.body.appendChild(panel);

    function switchTab(name) {
      panel.querySelectorAll('.side-panel-tab').forEach(function(t) {
        t.style.color = t.dataset.spTab === name ? 'var(--primary)' : 'var(--text-muted)';
        t.style.borderBottomColor = t.dataset.spTab === name ? 'var(--primary)' : 'transparent';
      });
      ['sp-users', 'sp-groups', 'sp-source'].forEach(function(id) {
        document.getElementById(id).style.display = id === 'sp-' + name ? 'block' : 'none';
      });
    }
    panel.querySelectorAll('.side-panel-tab').forEach(function(tab) {
      tab.addEventListener('click', function() { switchTab(tab.dataset.spTab); });
    });

    function closeP() { overlay.remove(); panel.remove(); }
    $('#sp-close').addEventListener('click', closeP);
    overlay.addEventListener('click', function(e) { if (e.target === overlay) closeP(); });
    $('#sp-bind-user').addEventListener('click', function() { showBindUserModal(skillName, function() { loadDetailData(skillName); }); });
    $('#sp-delete-skill').addEventListener('click', async function() {
      if (!await confirmModal('确定删除技能 ' + skillName + '？')) return;
      var res = await Api.post('/api/admin/skills/remove', { name: skillName });
      showMsg('#skills-msg', res.message || res.error, res.success);
      if (res.success) { closeP(); loadSkills(); }
    });

    await loadDetailData(skillName);
    async function loadDetailData(skillName) {
      var skillsData = await Api.get('/api/admin/skills?page=1&page_size=200');
      var skill = (skillsData.skills || []).find(function(s) { return s.name === skillName; });
      $('#sp-desc').textContent = skill ? (skill.description || '') : '';
      var skillSource = skill ? skill.source : '';
      var uData = await Api.get('/api/admin/users?page=1&page_size=200&runtime=false');
      var allUsers = (uData.users || []).filter(function(u) { return u.role !== 'superadmin'; });
      var directUsers = [], groupUsers = [];
      for (var u of allUsers) {
        var srcs = await Api.get('/api/admin/skills/user/sources?username=' + encodeURIComponent(u.username) + '&skill_name=' + encodeURIComponent(skillName)).catch(function() { return {}; });
        if (srcs.sources) { for (var s of srcs.sources) { if (s === 'direct') directUsers.push(u.username); else if (s.startsWith('group:')) groupUsers.push({ username: u.username, group: s.slice(6) }); } }
      }
      var uHtml = (directUsers.length === 0 && groupUsers.length === 0) ? '<p class="text-muted text-sm">暂无绑定用户</p>' : '';
      if (directUsers.length) {
        uHtml += '<div style="margin-bottom:.5rem"><strong>直接绑定</strong></div>';
        for (var du of directUsers) uHtml += '<div style="display:flex;align-items:center;justify-content:space-between;padding:.35rem 0;border-bottom:1px solid var(--border-light)"><span><span class="badge badge-info">直</span> ' + esc(du) + '</span><button class="btn btn-sm btn-danger" data-unbind-user="' + esc(du) + '">解绑</button></div>';
      }
      if (groupUsers.length) {
        uHtml += '<div style="margin:.5rem 0 .3rem"><strong>通过组继承</strong></div>';
        for (var gu of groupUsers) uHtml += '<div style="display:flex;align-items:center;justify-content:space-between;padding:.35rem 0;border-bottom:1px solid var(--border-light)"><span><span class="badge badge-success">组</span> ' + esc(gu.username) + ' (' + esc(gu.group) + ')</span><span class="text-muted text-sm">只读</span></div>';
      }
      $('#sp-users').innerHTML = uHtml;
      $('#sp-users').querySelectorAll('[data-unbind-user]').forEach(function(btn) {
        btn.addEventListener('click', async function() {
          if (!await confirmModal('确定从 ' + btn.dataset.unbindUser + ' 解绑？')) return;
          await Api.post('/api/admin/skills/user/unbind', { skill_name: skillName, username: btn.dataset.unbindUser });
          loadDetailData(skillName);
        });
      });

      var groupsData = await Api.get('/api/admin/groups?page=1&page_size=200');
      var boundGroups = [];
      for (var g of (groupsData.groups || [])) {
        var gs = await Api.get('/api/admin/groups/members?name=' + encodeURIComponent(g.name)).catch(function() { return {}; });
        if (gs.skills && gs.skills.indexOf(skillName) >= 0) boundGroups.push(g.name);
      }
      var gHtml = boundGroups.length === 0 ? '<p class="text-muted text-sm">暂无绑定组</p>' : '';
      for (var bg of boundGroups) gHtml += '<div style="display:flex;align-items:center;justify-content:space-between;padding:.35rem 0;border-bottom:1px solid var(--border-light)"><span>' + esc(bg) + '</span><button class="btn btn-sm btn-danger" data-unbind-group="' + esc(bg) + '">解绑</button></div>';
      $('#sp-groups').innerHTML = gHtml;
      $('#sp-groups').querySelectorAll('[data-unbind-group]').forEach(function(btn) {
        btn.addEventListener('click', async function() {
          if (!await confirmModal('确定从组 ' + btn.dataset.unbindGroup + ' 解绑？')) return;
          await Api.post('/api/admin/groups/skills/unbind', { group_name: btn.dataset.unbindGroup, skill_name: skillName });
          loadDetailData(skillName);
        });
      });

      var sHtml = skillSource ? '<div style="padding:.65rem .85rem;background:#f8fafc;border:1px solid var(--border-light);border-radius:var(--radius-sm);margin-bottom:.65rem"><div style="display:flex;justify-content:space-between;font-size:.85rem;padding:.25rem 0"><span style="color:var(--text-muted)">来源</span><span style="font-weight:600">' + esc(skillSource) + '</span></div></div>' : '<p class="text-muted text-sm">来源信息不可用</p>';
      $('#sp-source').innerHTML = sHtml;
    }
  }

  // ====================================================================
  // 绑定用户弹窗
  // ====================================================================

  async function showBindUserModal(skillName, onDone) {
    var uData = await Api.get('/api/admin/users?page=1&page_size=200&runtime=false').catch(function() { return {}; });
    var users = (uData.users || []).filter(function(u) { return u.role !== 'superadmin'; });
    var body = '<div class="field"><label>搜索</label><input type="search" id="bind-search" placeholder="输入用户名过滤"></div><div style="max-height:300px;overflow-y:auto" id="bind-list">';
    for (var u of users) body += '<label class="ckb-row" data-ckb-html="1"><input type="checkbox" class="ckb-input bind-cb" value="' + esc(u.username) + '"><span class="ckb-box"></span><span>' + esc(u.username) + '</span></label>';
    body += '</div>';
    showModal({ title: '绑定用户 — ' + skillName, body: body, footer: [{ label: '取消', value: 'cancel' }, { label: '确认绑定', value: 'bind', primary: true }] }).then(function(v) {
      if (v !== 'bind') return;
      var selected = []; document.querySelectorAll('.bind-cb:checked').forEach(function(cb) { selected.push(cb.value); });
      if (selected.length === 0) { showMsg('#skills-msg', '请选择至少一个用户', false); return; }
      Promise.all(selected.map(function(u) { return Api.post('/api/admin/skills/user/bind', { skill_name: skillName, username: u }); })).then(function(results) {
        showMsg('#skills-msg', '已绑定到 ' + results.filter(function(r) { return r.success; }).length + '/' + selected.length + ' 个用户', true);
        loadSkills(); if (onDone) onDone();
      });
    }).catch(function() {});
  }

  // ====================================================================
  // 单技能部署弹窗
  // ====================================================================

  function openSingleDeployModal(skillName) {
    var overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.innerHTML = '' +
      '<div class="modal" style="max-width:460px">' +
        '<div class="modal-header">部署 <strong>' + esc(skillName) + '</strong> <button class="modal-close-btn modal-cancel-trigger">&times;</button></div>' +
        '<div class="modal-body">' +
          '<div style="display:flex;gap:.35rem;margin-bottom:.65rem">' +
            '<button class="btn btn-sm deploy-target-mode active" data-smode="user" style="flex:1">按用户</button>' +
            '<button class="btn btn-sm deploy-target-mode" data-smode="group" style="flex:1">按组</button>' +
          '</div>' +
          '<div class="surface-panel" style="border:1px solid var(--border-light)">' +
            '<div style="padding:.5rem .75rem;border-bottom:1px solid var(--border-light)"><input type="search" id="single-target-search" placeholder="搜索..." style="width:100%;font-size:.82rem;padding:.3rem .5rem"></div>' +
            '<div id="single-target-list" style="max-height:260px;overflow-y:auto;padding:.3rem .5rem"></div>' +
          '</div>' +
          '<div style="margin-top:.65rem;font-size:.85rem;color:var(--text-muted)" id="single-summary">未选择目标</div>' +
        '</div>' +
        '<div class="modal-footer"><button class="btn btn-outline modal-cancel-trigger">取消</button><button class="btn btn-primary" id="single-deploy-confirm">部署</button></div>' +
      '</div>';
    document.body.appendChild(overlay);

    var mode = 'user';
    var targetList = $('#single-target-list');

    async function loadTargets() {
      var q = ($('#single-target-search').value || '').trim().toLowerCase();
      var checked = {}; targetList.querySelectorAll('.ckb-input:checked').forEach(function(cb) { checked[cb.value] = true; });
      targetList.innerHTML = '';
      if (mode === 'user') {
        var uData = await Api.get('/api/admin/users?page=1&page_size=200&runtime=false').catch(function() { return {}; });
        for (var u of ((uData.users || []).filter(function(u) { return u.role !== 'superadmin'; }))) {
          var val = 'user:' + u.username;
          if (q && u.username.toLowerCase().indexOf(q) < 0 && !checked[val]) continue;
          var row = makeCkbRow(val, u.username);
          if (checked[val]) { row.querySelector('.ckb-input').checked = true; row.classList.add('checked'); }
          targetList.appendChild(row);
        }
      } else {
        var gData = await Api.get('/api/admin/groups?page=1&page_size=200').catch(function() { return {}; });
        for (var g of (gData.groups || [])) {
          var val = 'group:' + g.name;
          if (q && g.name.toLowerCase().indexOf(q) < 0 && !checked[val]) continue;
          var row = makeCkbRow(val, g.name, ' <span class="badge badge-muted" style="font-size:.68rem">' + (g.member_count || 0) + '人</span>');
          if (checked[val]) { row.querySelector('.ckb-input').checked = true; row.classList.add('checked'); }
          targetList.appendChild(row);
        }
      }
      var count = targetList.querySelectorAll('.ckb-input:checked').length;
      $('#single-summary').textContent = count > 0 ? '已选择 ' + count + ' 个' + (mode === 'user' ? '用户' : '组') : '未选择目标';
      if (targetList.children.length === 0) targetList.innerHTML = '<p class="text-muted text-sm" style="padding:.5rem">无匹配' + (mode === 'user' ? '用户' : '组') + '</p>';
    }

    overlay.querySelectorAll('[data-smode]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        overlay.querySelectorAll('[data-smode]').forEach(function(b) { b.classList.remove('active'); });
        btn.classList.add('active'); mode = btn.dataset.smode; loadTargets();
      });
    });
    $('#single-target-search').addEventListener('input', loadTargets);

    $('#single-deploy-confirm').addEventListener('click', async function() {
      var targets = []; targetList.querySelectorAll('.ckb-input:checked').forEach(function(cb) { targets.push(cb.value); });
      if (targets.length === 0) { showMsg('#skills-msg', '请选择至少一个目标', false); return; }
      var btn = this; btn.disabled = true; btn.textContent = '部署中...';
      var done = 0, fail = 0; var source = findSkillSource(skillName);
        for (var t of targets) {
          var p = { skill_name: skillName, source: source };
          if (t.startsWith('user:')) p.username = t.slice(5);
          else if (t.startsWith('group:')) p.group_name = t.slice(6);
          var r = await Api.post('/api/admin/skills/deploy', p).catch(function() { return { success: false }; });
          if (r.success || r.task_id) done++;
          else if (r.error && r.error.indexOf('没有可部署的用户') >= 0) { /* 空组，跳过 */ done++; }
          else fail++;
        }
        showMsg('#skills-msg', '部署完成：成功 ' + done + '/' + targets.length + (fail > 0 ? '，失败 ' + fail : ''), done > 0);
        overlay.remove();
    });

    overlay.querySelectorAll('.modal-cancel-trigger').forEach(function(el) { el.addEventListener('click', function() { overlay.remove(); }); });
    overlay.addEventListener('click', function(e) { if (e.target === overlay) overlay.remove(); });
    loadTargets();
  }

  // ====================================================================
  // 批量部署引导弹窗
  // ====================================================================

  function openBatchDeployWizard() {
    var modal = $('#deploy-wizard');
    var step1 = $('#dw-step-1'), step2 = $('#dw-step-2'), step3 = $('#dw-step-3'), step4 = $('#dw-step-4');
    var nextBtn = $('#dw-next'), cancelBtn = $('#dw-cancel');
    var skillList = $('#dw-skill-list'), targetList = $('#dw-target-list');

    function reset() {
      step1.classList.remove('hidden'); step2.classList.add('hidden'); step3.classList.add('hidden'); step4.classList.add('hidden');
      nextBtn.textContent = '下一步'; nextBtn.disabled = false; nextBtn.classList.remove('hidden');
      cancelBtn.textContent = '取消'; cancelBtn.classList.remove('hidden');
    }

    reset(); modal.style.display = 'flex';
    function closeM() { modal.style.display = 'none'; reset(); }
    $('#dw-close').onclick = closeM; cancelBtn.onclick = closeM;
    modal.onclick = function(e) { if (e.target === modal) closeM(); };

    // 步骤 1：选择技能
    function renderSkills() {
      var q = ($('#dw-skill-search').value || '').trim().toLowerCase();
      var data = window._lastSkillsData; if (!data) return;
      var checked = {}; skillList.querySelectorAll('.ckb-input:checked').forEach(function(cb) { checked[cb.value] = true; });
      skillList.innerHTML = '';
      for (var sk of (data.skills || [])) {
        if (q && sk.name.toLowerCase().indexOf(q) < 0 && (sk.description || '').toLowerCase().indexOf(q) < 0 && !checked[sk.name]) continue;
        var row = makeCkbRow(sk.name, sk.name, sk.source ? ' <span class="badge badge-muted" style="font-size:.68rem">' + esc(sk.source) + '</span>' : '');
        if (checked[sk.name]) { row.querySelector('.ckb-input').checked = true; row.classList.add('checked'); }
        skillList.appendChild(row);
      }
    }
    $('#dw-skill-search').addEventListener('input', renderSkills);
    renderSkills();

    // 步骤 2 相关变量
    var dwMode = 'all';

    nextBtn.onclick = async function() {
      if (step1.classList.contains('hidden') === false) {
        // 步骤 1 → 步骤 2
        var selected = []; skillList.querySelectorAll('.ckb-input:checked').forEach(function(cb) { selected.push(cb.value); });
        if (selected.length === 0) { showMsg('#skills-msg', '请选择至少一个技能', false); return; }
        step1.classList.add('hidden'); step2.classList.remove('hidden');

        $('#dw-target-search-wrap').classList.add('hidden');
        targetList.innerHTML = '<p class="text-muted text-sm" style="padding:.5rem">部署到所有用户</p>';
        $('#dw-summary').textContent = selected.length + ' 个技能';
      } else if (step2.classList.contains('hidden') === false) {
        // 步骤 2 → 部署
        var selectedSkills = []; skillList.querySelectorAll('.ckb-input:checked').forEach(function(cb) { selectedSkills.push(cb.value); });
        var targets = [];
        if (dwMode === 'all') { targets = [{ type: 'all' }]; }
        else { targetList.querySelectorAll('.ckb-input:checked').forEach(function(cb) { targets.push(cb.value); }); }
        if (targets.length === 0) { showMsg('#skills-msg', '请选择至少一个目标', false); return; }

        step2.classList.add('hidden'); step3.classList.remove('hidden');
        nextBtn.classList.add('hidden'); cancelBtn.textContent = '后台运行';
        var total = selectedSkills.length * targets.length, done = 0, fail = 0;
        var fill = $('#dw-progress-fill'), ptext = $('#dw-progress-text'), pdetail = $('#dw-progress-detail');

        for (var sk of selectedSkills) {
          var source = findSkillSource(sk);
          for (var t of targets) {
            var p = { skill_name: sk, source: source };
            if (typeof t === 'string') {
              if (t.startsWith('user:')) p.username = t.slice(5);
              else if (t.startsWith('group:')) p.group_name = t.slice(6);
            }
            var r = await Api.post('/api/admin/skills/deploy', p).catch(function() { return { success: false }; });
            if (r.success || r.task_id) done++;
            else if (r.error && r.error.indexOf('没有可部署的用户') >= 0) done++;
            else fail++;
            var pct = Math.round((done + fail) / total * 100);
            fill.style.width = pct + '%';
            ptext.textContent = done + '/' + total + ' 完成';
            pdetail.textContent = '失败 ' + fail;
          }
        }

        step3.classList.add('hidden'); step4.classList.remove('hidden');
        $('#dw-done-icon').textContent = fail === 0 ? '✅' : '⚠️';
        $('#dw-done-title').textContent = fail === 0 ? '全部部署完成' : '部分部署失败';
        $('#dw-done-desc').textContent = '成功 ' + done + '/' + total + (fail > 0 ? '，失败 ' + fail : '');
        nextBtn.textContent = '完成'; nextBtn.classList.remove('hidden'); nextBtn.disabled = false;
        showMsg('#skills-msg', '批量部署完成：成功 ' + done + '/' + total + (fail > 0 ? '，失败 ' + fail : ''), done > 0);
        loadSkills();
      } else {
        closeM();
      }
    };

    // 步骤 2：目标选择
    var dwModeBtns = document.querySelectorAll('[data-dwmode]');
    dwModeBtns.forEach(function(btn) {
      btn.addEventListener('click', function() {
        dwModeBtns.forEach(function(b) { b.classList.remove('active'); });
        btn.classList.add('active'); dwMode = btn.dataset.dwmode;
        $('#dw-target-search-wrap').classList.toggle('hidden', dwMode === 'all');
        loadDWTargets();
      });
    });
    $('#dw-target-search').addEventListener('input', loadDWTargets);

    async function loadDWTargets() {
      var q = ($('#dw-target-search').value || '').trim().toLowerCase();
      var checked = {}; targetList.querySelectorAll('.ckb-input:checked').forEach(function(cb) { checked[cb.value] = true; });
      targetList.innerHTML = '';
      if (dwMode === 'all') { targetList.innerHTML = '<p class="text-muted text-sm" style="padding:.5rem">部署到所有用户</p>'; return; }

      if (dwMode === 'user') {
        var uData = await Api.get('/api/admin/users?page=1&page_size=200&runtime=false').catch(function() { return {}; });
        for (var u of ((uData.users || []).filter(function(u) { return u.role !== 'superadmin'; }))) {
          var val = 'user:' + u.username;
          if (q && u.username.toLowerCase().indexOf(q) < 0 && !checked[val]) continue;
          var row = makeCkbRow(val, u.username);
          if (checked[val]) { row.querySelector('.ckb-input').checked = true; row.classList.add('checked'); }
          targetList.appendChild(row);
        }
      } else {
        var gData = await Api.get('/api/admin/groups?page=1&page_size=200').catch(function() { return {}; });
        for (var g of (gData.groups || [])) {
          var val = 'group:' + g.name;
          if (q && g.name.toLowerCase().indexOf(q) < 0 && !checked[val]) continue;
          var row = makeCkbRow(val, g.name, ' <span class="badge badge-muted" style="font-size:.68rem">' + (g.member_count || 0) + '人</span>');
          if (checked[val]) { row.querySelector('.ckb-input').checked = true; row.classList.add('checked'); }
          targetList.appendChild(row);
        }
      }
      if (targetList.children.length === 0) targetList.innerHTML = '<p class="text-muted text-sm" style="padding:.5rem">无匹配' + (dwMode === 'user' ? '用户' : '组') + '</p>';
    }
  }
}
