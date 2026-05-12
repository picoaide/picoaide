function inferGitMode(url) {
  var lower = (url || '').trim().toLowerCase();
  if (lower.startsWith('git@') || lower.startsWith('ssh://')) return 'ssh';
  if (lower.startsWith('http://')) return 'http';
  return 'https';
}

function gitModeLabel(url) {
  var mode = inferGitMode(url);
  if (mode === 'ssh') return 'SSH 私钥';
  if (mode === 'http') return 'HTTP 用户名 + Token/密码';
  return 'HTTPS 用户名 + Token/密码';
}

function skillRepoCredentialTemplate(repoKey, idx, cred, repoURL) {
  var name = cred && cred.name ? cred.name : ('凭据 ' + (idx + 1));
  return '' +
    '<div class="repo-credential-row">' +
      '<div class="row" style="gap:.5rem;align-items:center;margin-bottom:.55rem">' +
        '<strong style="flex:1">凭据 ' + (idx + 1) + '</strong>' +
        '<span class="badge badge-muted" data-cred-mode-label>' + esc(gitModeLabel(repoURL)) + '</span>' +
        '<button class="btn btn-sm btn-danger" data-rm-cred="' + esc(repoKey) + ':' + idx + '">删除</button>' +
      '</div>' +
      '<div class="grid-2">' +
        '<div class="field"><label>名称</label><input type="text" data-repo-cred-field="name" data-repo="' + esc(repoKey) + '" data-idx="' + idx + '" value="' + esc(name) + '"></div>' +
        '<div class="field"><label>来源</label><input type="text" data-repo-cred-field="provider" data-repo="' + esc(repoKey) + '" data-idx="' + idx + '" value="' + esc((cred && cred.provider) || '') + '" placeholder="GitHub / GitLab / Gitee"></div>' +
      '</div>' +
      '<div class="field"><label>用户名</label><input type="text" data-repo-cred-field="username" data-repo="' + esc(repoKey) + '" data-idx="' + idx + '" value="' + esc((cred && cred.username) || '') + '" placeholder="HTTP/HTTPS 可填，SSH 可留空"></div>' +
      '<div class="field"><label>密钥 / Token / SSH 私钥</label><textarea rows="4" data-repo-cred-field="secret" data-repo="' + esc(repoKey) + '" data-idx="' + idx + '" placeholder="Token、密码或完整 SSH 私钥内容">' + esc((cred && cred.secret) || '') + '</textarea></div>' +
    '</div>';
}

function skillRepoCard(repo) {
  var creds = Array.isArray(repo.credentials) ? repo.credentials : [];
  var useCredentials = !repo.public;
  var checked = useCredentials ? ' checked' : '';
  var hidden = useCredentials ? '' : ' hidden';
  var refType = repo.ref_type || 'branch';
  var refBranch = refType === 'branch' ? ' selected' : '';
  var refTag = refType === 'tag' ? ' selected' : '';
  var credHtml = creds.map(function(cred, idx) { return skillRepoCredentialTemplate(repo.name, idx, cred, repo.url); }).join('');
  return '' +
    '<div class="card" data-repo-card="' + esc(repo.name) + '">' +
      '<div class="card-header" style="align-items:flex-start">' +
        '<div>' +
          '<div style="font-weight:700">' + esc(repo.name) + '</div>' +
          '<small class="text-muted">' + esc(repo.url || '') + '</small>' +
        '</div>' +
        '<div class="btn-group">' +
          '<button class="btn btn-sm btn-outline" data-repo-pull="' + esc(repo.name) + '">保存并更新</button>' +
          '<button class="btn btn-sm btn-danger" data-rm-repo="' + esc(repo.name) + '">移除来源</button>' +
        '</div>' +
      '</div>' +
      '<div class="grid-2">' +
        '<div class="field"><label>Git 地址</label><input type="text" data-repo-field="url" data-repo="' + esc(repo.name) + '" value="' + esc(repo.url || '') + '"></div>' +
        '<div class="field"><label>分支 / Tag</label><div class="row">' +
          '<select data-repo-field="ref_type" data-repo="' + esc(repo.name) + '" style="min-width:120px">' +
            '<option value="branch"' + refBranch + '>分支</option>' +
            '<option value="tag"' + refTag + '>Tag</option>' +
          '</select>' +
          '<input type="text" data-repo-field="ref" data-repo="' + esc(repo.name) + '" value="' + esc(repo.ref || '') + '" placeholder="main / v1.2.3" style="flex:1">' +
        '</div></div>' +
      '</div>' +
      '<div class="field">' +
        '<label class="toggle-switch toggle-switch-field">' +
          '<input type="checkbox" data-repo-field="use_credentials" data-repo="' + esc(repo.name) + '"' + checked + '>' +
          '<span class="toggle-switch-control" aria-hidden="true"></span>' +
          '<span class="toggle-switch-label">添加凭据</span>' +
        '</label>' +
      '</div>' +
      '<div class="field' + hidden + '" data-cred-wrap="' + esc(repo.name) + '">' +
        '<div class="row" style="justify-content:space-between;align-items:center;margin-bottom:.5rem">' +
          '<strong>凭据</strong>' +
          '<button class="btn btn-sm btn-outline" data-add-cred="' + esc(repo.name) + '">+ 添加凭据</button>' +
        '</div>' +
        '<div data-cred-list="' + esc(repo.name) + '">' + credHtml + '</div>' +
        '<div class="text-sm text-muted" style="margin-top:.4rem">可添加多个凭据，系统会按顺序尝试。凭据方式根据 Git 地址自动判断。</div>' +
      '</div>' +
      '<div class="card-footer">' +
        '<div class="row" style="justify-content:flex-end">' +
          '<button class="btn btn-primary btn-sm" data-save-repo="' + esc(repo.name) + '">保存配置</button>' +
        '</div>' +
      '</div>' +
    '</div>';
}

function collectCredentials(root, selectorPrefix, repoURL) {
  var credentials = [];
  var prefix = selectorPrefix ? selectorPrefix + ' ' : '';
  root.querySelectorAll(prefix + '[data-repo-cred-field="name"]').forEach(function(input) {
    var idx = Number(input.dataset.idx);
    if (!credentials[idx]) credentials[idx] = {};
    credentials[idx].name = input.value.trim();
  });
  root.querySelectorAll(prefix + '[data-repo-cred-field="provider"]').forEach(function(input) {
    var idx = Number(input.dataset.idx);
    if (!credentials[idx]) credentials[idx] = {};
    credentials[idx].provider = input.value.trim();
  });
  root.querySelectorAll(prefix + '[data-repo-cred-field="username"]').forEach(function(input) {
    var idx = Number(input.dataset.idx);
    if (!credentials[idx]) credentials[idx] = {};
    credentials[idx].username = input.value.trim();
  });
  root.querySelectorAll(prefix + '[data-repo-cred-field="secret"]').forEach(function(input) {
    var idx = Number(input.dataset.idx);
    if (!credentials[idx]) credentials[idx] = {};
    credentials[idx].secret = input.value;
  });
  return credentials.filter(function(item) {
    return item && (item.name || item.provider || item.username || item.secret);
  }).map(function(item) {
    item.mode = inferGitMode(repoURL);
    return item;
  });
}

function readRepoCard(card) {
  var name = card.dataset.repoCard;
  var url = card.querySelector('[data-repo-field="url"]').value.trim();
  var useCredentials = card.querySelector('[data-repo-field="use_credentials"]').checked;
  return {
    name: name,
    url: url,
    ref: card.querySelector('[data-repo-field="ref"]').value.trim(),
    ref_type: card.querySelector('[data-repo-field="ref_type"]').value,
    public: !useCredentials,
    credentials: useCredentials ? collectCredentials(card, '', url) : [],
  };
}

export async function init(ctx) {
  const { Api, esc, showMsg } = ctx;
  let skillsPage = 1;
  let skillsPageSize = 50;
  let skillsTotalPages = 1;
  let skillsSearch = '';

  await loadSkills();
  await loadRepos();
  ctx.$('#deploy-btn').addEventListener('click', deploySkills);
  ctx.$('#skills-search-btn')?.addEventListener('click', () => {
    skillsSearch = ctx.$('#skills-search').value.trim();
    skillsPage = 1;
    loadSkills();
  });
  ctx.$('#skills-search')?.addEventListener('keydown', e => {
    if (e.key === 'Enter') {
      skillsSearch = e.target.value.trim();
      skillsPage = 1;
      loadSkills();
    }
  });
  ctx.$('#skills-page-size')?.addEventListener('change', e => {
    skillsPageSize = Number(e.target.value) || 50;
    skillsPage = 1;
    loadSkills();
  });
  ctx.$('#skills-prev')?.addEventListener('click', () => {
    if (skillsPage > 1) {
      skillsPage--;
      loadSkills();
    }
  });
  ctx.$('#skills-next')?.addEventListener('click', () => {
    if (skillsPage < skillsTotalPages) {
      skillsPage++;
      loadSkills();
    }
  });
  ctx.$('#deploy-target-type')?.addEventListener('change', loadDeployTargets);
  ctx.$('#deploy-target-search-btn')?.addEventListener('click', loadDeployTargets);
  ctx.$('#deploy-target-search')?.addEventListener('keydown', e => {
    if (e.key === 'Enter') loadDeployTargets();
  });
  ctx.$('#skill-add-btn')?.addEventListener('click', addSkill);
  ctx.$('[data-skill-source="zip"]')?.addEventListener('click', () => setSkillSource('zip'));
  ctx.$('[data-skill-source="git"]')?.addEventListener('click', () => setSkillSource('git'));
  ctx.$('#skill-upload-file')?.addEventListener('change', function() {
    var file = this.files && this.files[0];
    ctx.$('#skill-upload-file-name').textContent = file ? file.name : '未选择 zip 文件';
  });
  ctx.$('#repo-use-credentials')?.addEventListener('change', function() {
    ctx.$('#repo-new-credentials').classList.toggle('hidden', !this.checked);
  });
  ctx.$('#repo-url')?.addEventListener('input', function() {
    updateModeLabels(ctx.$('#repo-new-cred-list'), this.value);
  });
  ctx.$('#repo-new-add-cred')?.addEventListener('click', function() {
    var list = ctx.$('#repo-new-cred-list');
    var idx = list.children.length;
    var tmp = document.createElement('div');
    tmp.innerHTML = skillRepoCredentialTemplate('__new__', idx, {}, ctx.$('#repo-url').value);
    list.appendChild(tmp.firstElementChild);
    bindNewCredentialEvents();
  });

  function setSkillSource(source) {
    ctx.$('#skill-source').value = source;
    ctx.$$('[data-skill-source]').forEach(function(btn) {
      btn.classList.toggle('active', btn.dataset.skillSource === source);
    });
    ctx.$('#skill-source-zip')?.classList.toggle('hidden', source !== 'zip');
    ctx.$('#skill-source-git')?.classList.toggle('hidden', source !== 'git');
  }

  async function loadSkills() {
    const tbody = ctx.$('#skills-tbody');
    const empty = ctx.$('#skills-empty');
    tbody.innerHTML = '';
    empty.classList.add('hidden');

    const params = new URLSearchParams({ page: String(skillsPage), page_size: String(skillsPageSize) });
    if (skillsSearch) params.set('search', skillsSearch);
    const data = await Api.get('/api/admin/skills?' + params.toString());
    if (!data.success) return;
    skillsPage = data.page || skillsPage;
    skillsPageSize = data.page_size || skillsPageSize;
    skillsTotalPages = data.total_pages || 1;
    updateSkillsPager(data);

    const skills = data.skills || [];
    if (skills.length === 0) { empty.classList.remove('hidden'); }
    else {
      for (const sk of skills) {
        const tr = document.createElement('tr');
        tr.innerHTML = '<td><strong>' + esc(sk.name) + '</strong></td><td>' + sk.file_count + ' 文件</td><td>' + sk.size_str + '</td><td>' + sk.mod_time + '</td><td class="actions-cell"><div class="btn-group"><button class="btn btn-sm btn-outline" data-dl="' + esc(sk.name) + '">下载</button><button class="btn btn-sm btn-danger" data-rm="' + esc(sk.name) + '">删除</button></div></td>';
        tbody.appendChild(tr);
      }
    }

    tbody.querySelectorAll('[data-dl]').forEach(btn => {
      btn.addEventListener('click', async () => {
        const base = await getServerUrl();
        window.open(base + '/api/admin/skills/download?name=' + encodeURIComponent(btn.dataset.dl), '_blank');
      });
    });
    tbody.querySelectorAll('[data-rm]').forEach(btn => {
      btn.addEventListener('click', async () => {
        if (!await confirmModal('删除技能 ' + btn.dataset.rm + '？')) return;
        const res = await Api.post('/api/admin/skills/remove', { name: btn.dataset.rm });
        showMsg('#skills-msg', res.message || res.error, res.success);
        if (res.success) loadSkills();
      });
    });

    const skillSel = ctx.$('#deploy-skill');
    skillSel.innerHTML = '<option value="">所有技能</option>';
    for (const sk of skills) skillSel.innerHTML += '<option value="' + esc(sk.name) + '">' + esc(sk.name) + '</option>';

    await loadDeployTargets();
  }

  function updateSkillsPager(data) {
    const info = ctx.$('#skills-page-info');
    if (info) info.textContent = '第 ' + (data.page || 1) + ' / ' + (data.total_pages || 1) + ' 页，共 ' + (data.total || 0) + ' 个技能';
    const sizeSel = ctx.$('#skills-page-size');
    if (sizeSel) sizeSel.value = String(skillsPageSize);
    const prev = ctx.$('#skills-prev');
    const next = ctx.$('#skills-next');
    if (prev) prev.disabled = skillsPage <= 1;
    if (next) next.disabled = skillsPage >= skillsTotalPages;
  }

  async function loadDeployTargets() {
    const select = ctx.$('#deploy-target');
    const type = ctx.$('#deploy-target-type')?.value || 'all';
    const q = ctx.$('#deploy-target-search')?.value.trim() || '';
    select.innerHTML = '<option value="all">所有用户</option>';
    if (type === 'all') return;
    const params = new URLSearchParams({ page: '1', page_size: '100' });
    if (q) params.set('search', q);
    if (type === 'user') {
      params.set('runtime', 'false');
      const uData = await Api.get('/api/admin/users?' + params.toString()).catch(() => ({}));
      for (const u of (uData.users || [])) {
        if (u.role === 'superadmin') continue;
        const opt = document.createElement('option');
        opt.value = 'user:' + u.username;
        opt.textContent = u.username;
        select.appendChild(opt);
      }
    } else if (type === 'group') {
      const gData = await Api.get('/api/admin/groups?' + params.toString()).catch(() => ({}));
      for (const g of (gData.groups || [])) {
        const opt = document.createElement('option');
        opt.value = 'group:' + g.name;
        opt.textContent = g.name + ' (' + g.member_count + '人)';
        select.appendChild(opt);
      }
    }
  }

  async function uploadSkillZip(name) {
    var input = ctx.$('#skill-upload-file');
    var file = input && input.files && input.files[0];
    if (!name || !file) {
      showMsg('#skills-msg', '请输入技能名称并选择 zip 文件', false);
      return;
    }
    showMsg('#skills-msg', '正在上传技能...', true);
    try {
      var csrf = await getCSRF();
      var form = new FormData();
      form.append('name', name);
      form.append('file', file);
      form.append('csrf_token', csrf);
      var base = await getServerUrl();
      var resp = await fetch(base + '/api/admin/skills/upload', {
        method: 'POST',
        credentials: 'include',
        body: form,
      });
      var res = await resp.json();
      showMsg('#skills-msg', res.message || res.error, !!res.success);
      if (res.success) {
        input.value = '';
        ctx.$('#skill-upload-file-name').textContent = '未选择 zip 文件';
        loadSkills();
      }
    } catch (e) { showMsg('#skills-msg', e.message, false); }
  }

  async function deploySkills() {
    const params = {};
    const skill = ctx.$('#deploy-skill').value;
    const target = ctx.$('#deploy-target').value;
    if (skill) params.skill_name = skill;
    if (target && target !== 'all') {
      if (target.startsWith('user:')) params.username = target.slice(5);
      else if (target.startsWith('group:')) params.group_name = target.slice(6);
    }
    try {
      const res = await Api.post('/api/admin/skills/deploy', params);
      if (res.task_id) {
        pollDeployStatus(res.message);
      } else {
        showMsg('#deploy-msg', res.message || res.error, res.success);
      }
    } catch (e) { showMsg('#deploy-msg', e.message, false); }
  }

  function pollDeployStatus(initialMsg) {
    showMsg('#deploy-msg', initialMsg + '...', true);
    var poll = async function() {
      try {
        var st = await Api.get('/api/admin/task/status');
        if (st.running) {
          var pct = st.total > 0 ? Math.round((st.done + st.failed) / st.total * 100) : 0;
          showMsg('#deploy-msg', initialMsg + ' ' + (st.done + st.failed) + '/' + st.total + ' (' + pct + '%)', true);
          setTimeout(poll, 2000);
        } else {
          showMsg('#deploy-msg', st.message || '完成', st.failed === 0);
        }
      } catch (e) {
        showMsg('#deploy-msg', '查询进度失败: ' + e.message, false);
      }
    };
    setTimeout(poll, 1500);
  }

  async function loadRepos() {
    const list = ctx.$('#repos-list');
    list.innerHTML = '';
    const data = await Api.get('/api/admin/skills');
    const repos = data.repos || [];

    if (repos.length === 0) { list.innerHTML = '<small>暂无 Git 来源技能</small>'; return; }

    for (const repo of repos) {
      const wrap = document.createElement('div');
      wrap.innerHTML = skillRepoCard(repo);
      list.appendChild(wrap.firstElementChild);
    }

    bindRepoEvents();
  }

  function bindRepoEvents() {
    ctx.$$('#repos-list [data-repo-card]').forEach(card => {
      var repoName = card.dataset.repoCard;
      if (card.dataset.bound === '1') return;
      card.dataset.bound = '1';
      var urlInput = card.querySelector('[data-repo-field="url"]');
      var useCredInput = card.querySelector('[data-repo-field="use_credentials"]');
      useCredInput.addEventListener('change', function() {
        card.querySelector('[data-cred-wrap="' + CSS.escape(repoName) + '"]').classList.toggle('hidden', !this.checked);
      });
      urlInput.addEventListener('input', function() {
        updateModeLabels(card.querySelector('[data-cred-list="' + CSS.escape(repoName) + '"]'), this.value);
      });
      card.querySelector('[data-add-cred]').addEventListener('click', function() {
        var list = card.querySelector('[data-cred-list="' + CSS.escape(repoName) + '"]');
        var idx = list.children.length;
        var tmp = document.createElement('div');
        tmp.innerHTML = skillRepoCredentialTemplate(repoName, idx, {}, urlInput.value);
        list.appendChild(tmp.firstElementChild);
        bindCredentialDeleteEvents(card, repoName);
      });
      bindCredentialDeleteEvents(card, repoName);
      card.querySelectorAll('[data-repo-pull]').forEach(btn => {
        btn.addEventListener('click', async function() {
          const repo = readRepoCard(card);
          const saved = await Api.post('/api/admin/skills/repos/save', { repo: JSON.stringify(repo) });
          if (!saved.success) {
            showMsg('#repo-msg', saved.message || saved.error, false);
            return;
          }
          const res = await Api.post('/api/admin/skills/repos/pull', { name: btn.dataset.repoPull });
          showMsg('#repo-msg', res.message || res.error, res.success);
          if (res.success) loadRepos();
        });
      });
      card.querySelectorAll('[data-rm-repo]').forEach(btn => {
        btn.addEventListener('click', async function() {
          if (!await confirmModal('移除 Git 来源 ' + btn.dataset.rmRepo + '？已安装技能不会被删除。')) return;
          const res = await Api.post('/api/admin/skills/repos/remove', { name: btn.dataset.rmRepo });
          showMsg('#repo-msg', res.message || res.error, res.success);
          if (res.success) loadRepos();
        });
      });
      card.querySelectorAll('[data-save-repo]').forEach(btn => {
        btn.addEventListener('click', async function() {
          const repo = readRepoCard(card);
          const res = await Api.post('/api/admin/skills/repos/save', { repo: JSON.stringify(repo) });
          showMsg('#repo-msg', res.message || res.error, res.success);
          if (res.success) loadRepos();
        });
      });
    });
  }

  function updateModeLabels(container, repoURL) {
    if (!container) return;
    container.querySelectorAll('[data-cred-mode-label]').forEach(function(label) {
      label.textContent = gitModeLabel(repoURL);
    });
  }

  function bindCredentialDeleteEvents(card, repoName) {
    card.querySelectorAll('[data-rm-cred]').forEach(btn => {
      if (btn.dataset.bound === '1') return;
      btn.dataset.bound = '1';
      btn.addEventListener('click', function() {
        var parts = btn.dataset.rmCred.split(':');
        var idx = Number(parts[1]);
        var list = card.querySelector('[data-cred-list="' + CSS.escape(repoName) + '"]');
        var item = list.children[idx];
        if (item) item.remove();
        reindexCredentials(card, repoName);
      });
    });
  }

  function bindNewCredentialEvents() {
    ctx.$$('#repo-new-cred-list [data-rm-cred]').forEach(function(btn) {
      if (btn.dataset.bound === '1') return;
      btn.dataset.bound = '1';
      btn.addEventListener('click', function() {
        var item = btn.closest('.repo-credential-row');
        if (item) item.remove();
        reindexNewCredentials();
      });
    });
  }

  function reindexNewCredentials() {
    var list = ctx.$('#repo-new-cred-list');
    Array.from(list.children).forEach(function(child, idx) {
      child.querySelectorAll('[data-repo-cred-field]').forEach(function(input) {
        input.dataset.idx = String(idx);
      });
      var rm = child.querySelector('[data-rm-cred]');
      if (rm) rm.dataset.rmCred = '__new__:' + idx;
      var title = child.querySelector('strong');
      if (title) title.textContent = '凭据 ' + (idx + 1);
    });
  }

  function readNewCredentials(repoURL) {
    return collectCredentials(document, '#repo-new-cred-list', repoURL);
  }

  function reindexCredentials(card, repoName) {
    var list = card.querySelector('[data-cred-list="' + CSS.escape(repoName) + '"]');
    Array.from(list.children).forEach(function(child, idx) {
      child.querySelectorAll('[data-repo-cred-field]').forEach(function(input) {
        input.dataset.idx = String(idx);
      });
      var rm = child.querySelector('[data-rm-cred]');
      if (rm) rm.dataset.rmCred = repoName + ':' + idx;
      var title = child.querySelector('strong');
      if (title) title.textContent = '凭据 ' + (idx + 1);
    });
  }

  async function addSkill() {
    const source = ctx.$('#skill-source').value;
    const name = ctx.$('#skill-name').value.trim();
    if (!name) { showMsg('#repo-msg', '请输入技能名称', false); return; }
    if (source === 'zip') {
      await uploadSkillZip(name);
      if (ctx.$('#skill-upload-file').value === '') ctx.$('#skill-name').value = '';
      return;
    }
    const url = ctx.$('#repo-url').value.trim();
    const useCredentials = ctx.$('#repo-use-credentials').checked;
    if (!url) { showMsg('#repo-msg', '请输入 Git 地址', false); return; }
    const repo = {
      name: name,
      url: url,
      ref: ctx.$('#repo-ref').value.trim(),
      ref_type: ctx.$('#repo-ref-type').value,
      public: !useCredentials,
      credentials: useCredentials ? readNewCredentials(url) : [],
    };
    const res = await Api.post('/api/admin/skills/repos/add', { repo: JSON.stringify(repo) });
    showMsg('#repo-msg', res.message || res.error, res.success);
    if (res.success) {
      ctx.$('#skill-name').value = '';
      ctx.$('#repo-url').value = '';
      ctx.$('#repo-ref').value = '';
      ctx.$('#repo-ref-type').value = 'branch';
      ctx.$('#repo-use-credentials').checked = false;
      ctx.$('#repo-new-credentials').classList.add('hidden');
      ctx.$('#repo-new-cred-list').innerHTML = '';
      loadSkills();
      loadRepos();
    }
  }
}

async function getServerUrl() {
  return window.location.origin.replace(/\/+$/, '');
}
