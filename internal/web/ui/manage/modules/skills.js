export async function init(ctx) {
  var { Api, esc, showMsg, $ } = ctx;

  var searchQuery = '';

  // 明快色板 — 饱和但沉稳，白色字母在其中清晰可辨
  var iconColors = ['#2563eb','#7c3aed','#db2777','#dc2626','#d97706','#059669','#0891b2','#4f46e5','#9333ea','#0d9488'];

  function skillIcon(name) {
    var idx = name.charCodeAt(0) || 0;
    var color = iconColors[idx % iconColors.length];
    var letter = name.charAt(0).toUpperCase();
    return '<div style="width:50px;height:50px;border-radius:12px;background:' + color + ';color:#fff;display:flex;align-items:center;justify-content:center;font-weight:800;font-size:1.25rem;flex-shrink:0;box-shadow:0 3px 8px ' + color + '55">' + esc(letter) + '</div>';
  }

  async function loadSkills() {
    var grid = $('#skills-grid');
    var empty = $('#skills-empty');
    grid.innerHTML = '';
    empty.classList.add('hidden');

    var data = await Api.get('/api/user/skills');
    if (!data.success) {
      showMsg('#skills-msg', data.error || '加载失败', false);
      return;
    }

    var skills = data.skills || [];
    if (skills.length === 0) {
      empty.classList.remove('hidden');
      return;
    }

    var q = searchQuery.toLowerCase();
    var filtered = skills.filter(function(sk) {
      if (!q) return true;
      return sk.name.toLowerCase().indexOf(q) >= 0 ||
        (sk.description || '').toLowerCase().indexOf(q) >= 0;
    });

    if (filtered.length === 0) {
      grid.innerHTML = '<div class="text-muted text-center" style="grid-column:1/-1;padding:2rem">没有匹配的技能</div>';
      return;
    }

    for (var sk of filtered) {
      var card = document.createElement('div');

      var statusBadge = '';
      var actionHtml = '';

      if (sk.install_status === 'installed' && sk.user_installed) {
        statusBadge = '<span class="badge badge-success" style="font-size:.78rem;padding:.2rem .6rem">已安装</span>';
        actionHtml = '<button class="btn btn-sm btn-outline btn-danger uninstall-btn" data-name="' + esc(sk.name) + '">卸载</button>';
      } else if (sk.install_status === 'installed' && !sk.user_installed) {
        statusBadge = '<span class="badge badge-info" style="font-size:.78rem;padding:.2rem .6rem">已部署</span>';
        actionHtml = '<span class="text-muted text-sm" style="font-size:.78rem">管理员部署</span>';
      } else {
        statusBadge = '<span class="badge" style="font-size:.78rem;padding:.2rem .6rem">未安装</span>';
        actionHtml = '<button class="btn btn-sm btn-primary install-btn" data-name="' + esc(sk.name) + '">安装</button>';
      }

      var footerMeta = '';
      if (sk.source || sk.size_str) {
        var parts = [];
        if (sk.source) parts.push('<span style="display:inline-flex;align-items:center;gap:4px"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/><path d="M2 12h20"/></svg>' + esc(sk.source) + '</span>');
        if (sk.size_str) parts.push('<span style="display:inline-flex;align-items:center;gap:4px"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/></svg>' + esc(sk.size_str) + '</span>');
        footerMeta = parts.join('<span style="color:var(--border);margin:0 5px">|</span>');
      }

      var desc = sk.description || '';
      if (desc.length > 300) desc = desc.substring(0, 300) + '…';

      card.style.cssText = 'background:var(--surface);border:1px solid var(--border);border-radius:12px;overflow:hidden;transition:all .22s ease;box-shadow:0 1px 3px rgba(15,23,42,.04);display:flex;flex-direction:column;min-height:200px';
      card.onmouseenter = function() { this.style.boxShadow = '0 10px 32px rgba(15,23,42,.09)'; this.style.borderColor = '#cbd5e1'; this.style.transform = 'translateY(-3px)'; };
      card.onmouseleave = function() { this.style.boxShadow = '0 1px 3px rgba(15,23,42,.04)'; this.style.borderColor = 'var(--border)'; this.style.transform = 'none'; };

      card.innerHTML = '' +
        '<div style="padding:1.05rem 1rem .6rem;flex:1">' +
          '<div style="display:flex;align-items:flex-start;gap:.85rem">' +
            skillIcon(sk.name) +
            '<div style="flex:1;min-width:0">' +
              '<div style="display:flex;align-items:center;gap:.5rem;flex-wrap:wrap;margin-bottom:.25rem">' +
                '<span style="font-weight:700;font-size:1rem;color:var(--text)">' + esc(sk.name) + '</span>' +
                statusBadge +
              '</div>' +
              (desc ? '<div style="color:var(--text-secondary);font-size:.88rem;line-height:1.6;overflow:hidden;display:-webkit-box;-webkit-line-clamp:4;-webkit-box-orient:vertical">' + esc(desc) + '</div>' : '') +
            '</div>' +
          '</div>' +
        '</div>' +
        '<div style="padding:.5rem 1rem .75rem;border-top:1px solid var(--border-light);display:flex;align-items:center;justify-content:space-between;gap:.65rem">' +
          '<div style="display:flex;align-items:center;gap:.4rem;font-size:.78rem;color:var(--text-muted);flex-wrap:wrap;min-width:0">' +
            footerMeta +
          '</div>' +
          '<div style="flex-shrink:0">' + actionHtml + '</div>' +
        '</div>';

      grid.appendChild(card);
    }

    grid.querySelectorAll('.install-btn').forEach(function(btn) {
      btn.addEventListener('click', async function() {
        btn.disabled = true;
        btn.textContent = '安装中...';
        var res = await Api.post('/api/user/skills/install', { skill_name: btn.dataset.name });
        showMsg('#skills-msg', res.message || res.error, res.success);
        if (res.success) loadSkills();
        else { btn.disabled = false; btn.textContent = '安装'; }
      });
    });

    grid.querySelectorAll('.uninstall-btn').forEach(function(btn) {
      btn.addEventListener('click', async function() {
        btn.disabled = true;
        btn.textContent = '卸载中...';
        var res = await Api.post('/api/user/skills/uninstall', { skill_name: btn.dataset.name });
        showMsg('#skills-msg', res.message || res.error, res.success);
        if (res.success) loadSkills();
        else { btn.disabled = false; btn.textContent = '卸载'; }
      });
    });
  }

  $('#skills-search-btn').addEventListener('click', function() {
    searchQuery = $('#skills-search').value.trim();
    loadSkills();
  });
  $('#skills-search').addEventListener('keydown', function(e) {
    if (e.key === 'Enter') {
      searchQuery = e.target.value.trim();
      loadSkills();
    }
  });

  loadSkills();
}
