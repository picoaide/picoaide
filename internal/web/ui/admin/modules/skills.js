export async function init(ctx) {
  const { Api, esc, showMsg } = ctx;
  await loadSkills();
  await loadRepos();
  ctx.$('#deploy-btn').addEventListener('click', deploySkills);
  ctx.$('#repo-add-btn').addEventListener('click', addRepo);

  async function loadSkills() {
    const tbody = ctx.$('#skills-tbody');
    const empty = ctx.$('#skills-empty');
    tbody.innerHTML = '';
    empty.classList.add('hidden');

    const data = await Api.get('/api/admin/skills');
    if (!data.success) return;

    const skills = data.skills || [];
    if (skills.length === 0) { empty.classList.remove('hidden'); }
    else {
      for (const sk of skills) {
        const tr = document.createElement('tr');
        tr.innerHTML = '<td><strong>' + esc(sk.name) + '</strong></td><td>' + sk.file_count + ' 文件</td><td>' + sk.size_str + '</td><td>' + sk.mod_time + '</td><td><div class="btn-group"><button class="btn btn-sm btn-outline" data-dl="' + esc(sk.name) + '">下载</button><button class="btn btn-sm btn-danger" data-rm="' + esc(sk.name) + '">删除</button></div></td>';
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
        if (!confirm('删除技能 ' + btn.dataset.rm + '？')) return;
        const res = await Api.post('/api/admin/skills/remove', { name: btn.dataset.rm });
        showMsg('#skills-msg', res.message || res.error, res.success);
        if (res.success) loadSkills();
      });
    });

    const skillSel = ctx.$('#deploy-skill');
    skillSel.innerHTML = '<option value="">所有技能</option>';
    for (const sk of skills) skillSel.innerHTML += '<option value="' + esc(sk.name) + '">' + esc(sk.name) + '</option>';

    try {
      const uData = await Api.get('/api/admin/users');
      const userGroup = ctx.$('#deploy-user-group');
      userGroup.innerHTML = '';
      for (const u of (uData.users || [])) {
        if (u.role === 'superadmin') continue;
        const opt = document.createElement('option');
        opt.value = 'user:' + u.username;
        opt.textContent = u.username;
        userGroup.appendChild(opt);
      }
    } catch {}

    try {
      const gData = await Api.get('/api/admin/groups');
      const groupGroup = ctx.$('#deploy-group-group');
      groupGroup.innerHTML = '';
      const groups = gData.groups || [];
      // 构建树状结构
      const byParent = {};
      const byId = {};
      for (const g of groups) {
        byId[g.id] = g;
        const pid = g.parent_id || 'root';
        if (!byParent[pid]) byParent[pid] = [];
        byParent[pid].push(g);
      }
      function addTreeOptions(parentId, depth) {
        const children = byParent[parentId] || [];
        for (const g of children) {
          const opt = document.createElement('option');
          opt.value = 'group:' + g.name;
          opt.textContent = '\u00A0\u00A0'.repeat(depth) + (depth > 0 ? '└ ' : '') + g.name + ' (' + g.member_count + '人)';
          groupGroup.appendChild(opt);
          addTreeOptions(g.id, depth + 1);
        }
      }
      addTreeOptions('root', 0);
    } catch {}
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

    if (repos.length === 0) { list.innerHTML = '<small>暂无仓库</small>'; return; }

    for (const repo of repos) {
      const card = document.createElement('div');
      card.className = 'card';
      card.innerHTML = '<div class="card-header">' + esc(repo.name) + '</div><small>' + esc(repo.url) + '</small><br><small>上次拉取: ' + (repo.last_pull || '从未') + '</small><div class="card-footer"><div class="btn-group"><button class="btn btn-sm btn-outline" data-repo-skill="' + esc(repo.name) + '">查看技能</button><button class="btn btn-sm btn-outline" data-pull="' + esc(repo.name) + '">拉取更新</button><button class="btn btn-sm btn-danger" data-install="' + esc(repo.name) + '">安装全部</button><button class="btn btn-sm btn-outline" data-rm-repo="' + esc(repo.name) + '">删除仓库</button></div></div><div id="repo-skills-' + esc(repo.name) + '" class="mt-1"></div>';
      list.appendChild(card);
    }

    list.querySelectorAll('[data-repo-skill]').forEach(btn => {
      btn.addEventListener('click', async () => {
        const name = btn.dataset.repoSkill;
        const target = ctx.$('#repo-skills-' + CSS.escape(name));
        if (!target) return;
        if (target.innerHTML) { target.innerHTML = ''; return; }
        const data = await Api.get('/api/admin/skills/repos/list?name=' + encodeURIComponent(name));
        if (!data.success) return;
        const skills = data.skills || [];
        if (skills.length === 0) { target.innerHTML = '<small>无技能</small>'; return; }
        let html = '<div class="grid-auto">';
        for (const sk of skills) html += '<button class="btn btn-sm btn-outline" data-install-skill="' + esc(name) + ':' + esc(sk.name) + '">' + esc(sk.name) + (sk.installed ? ' ✅' : '') + '</button>';
        html += '</div>';
        target.innerHTML = html;
        target.querySelectorAll('[data-install-skill]').forEach(el => {
          el.addEventListener('click', async () => {
            const [repo, skill] = el.dataset.installSkill.split(':');
            const res = await Api.post('/api/admin/skills/install', { repo, skill });
            showMsg('#repo-msg', res.message || res.error, res.success);
            if (res.success) { loadSkills(); loadRepos(); }
          });
        });
      });
    });

    list.querySelectorAll('[data-pull]').forEach(btn => {
      btn.addEventListener('click', async () => {
        const res = await Api.post('/api/admin/skills/repos/pull', { name: btn.dataset.pull });
        showMsg('#repo-msg', res.message || res.error, res.success);
        if (res.success) loadRepos();
      });
    });
    list.querySelectorAll('[data-install]').forEach(btn => {
      btn.addEventListener('click', async () => {
        if (!confirm('安装此仓库中的所有技能到技能库？')) return;
        const res = await Api.post('/api/admin/skills/install', { repo: btn.dataset.install });
        showMsg('#repo-msg', res.message || res.error, res.success);
        if (res.success) { loadSkills(); loadRepos(); }
      });
    });
    list.querySelectorAll('[data-rm-repo]').forEach(btn => {
      btn.addEventListener('click', async () => {
        if (!confirm('删除仓库 ' + btn.dataset.rmRepo + '？')) return;
        const res = await Api.post('/api/admin/skills/repos/remove', { name: btn.dataset.rmRepo });
        showMsg('#repo-msg', res.message || res.error, res.success);
        if (res.success) loadRepos();
      });
    });
  }

  async function addRepo() {
    const name = ctx.$('#repo-name').value.trim();
    const url = ctx.$('#repo-url').value.trim();
    if (!name || !url) { showMsg('#repo-msg', '请输入仓库名称和地址', false); return; }
    const res = await Api.post('/api/admin/skills/repos/add', { name, url });
    showMsg('#repo-msg', res.message || res.error, res.success);
    if (res.success) { ctx.$('#repo-name').value = ''; ctx.$('#repo-url').value = ''; loadRepos(); }
  }
}

async function getServerUrl() {
  return window.location.origin.replace(/\/+$/, '');
}
