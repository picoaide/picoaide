var rawConfig = {};

export async function init(ctx) {
  const { Api, showMsg, $, $$ } = ctx;
  await loadConfig();

  $('#auth-mode').addEventListener('change', function() {
    var mode = this.value;
    var ldapOn = mode === 'ldap';
    if (ldapOn) {
      if (!confirm('切换到 LDAP 统一登录模式？\n\n启用后，本地用户（非超管）将被禁用，仅超管可使用本地密码登录。')) {
        this.value = 'local';
        return;
      }
    } else {
      if (!confirm('切换到本地认证模式？\n\nLDAP 用户将无法登录。')) {
        this.value = 'ldap';
        return;
      }
    }
    updateLDAPVisibility(ldapOn);
  });

  $('#save-auth-btn').addEventListener('click', saveAuthConfig);
  $('#save-ldap-btn').addEventListener('click', saveLDAPConfig);
  $('#test-ldap-btn').addEventListener('click', testLDAP);
  $('#save-group-config-btn').addEventListener('click', saveGroupConfig);
  $('#sync-groups-btn').addEventListener('click', syncLDAPGroups);
  $('#save-sync-btn').addEventListener('click', saveSyncConfig);
  $('#wl-enabled').addEventListener('change', updateWhitelistVisibility);
  $('#wl-add-btn').addEventListener('click', addWhitelistUser);
  $('#wl-search-btn').addEventListener('click', searchLDAPUsers);

  async function loadConfig() {
    showMsg('#auth-msg', '加载中...', true);
    try {
      var base = await getServerUrl();
      var resp = await fetch(base + '/api/config', { method: 'GET', credentials: 'include' });
      var text = await resp.text();
      try { var e = JSON.parse(text); if (e.success === false) { showMsg('#auth-msg', e.error, false); return; } } catch {}
      rawConfig = JSON.parse(text);
      showMsg('#auth-msg', '');
      renderConfig();
    } catch (e) { showMsg('#auth-msg', e.message, false); }
  }

  function renderConfig() {
    var ldapOn = rawConfig.web?.ldap_enabled === true || rawConfig.web?.ldap_enabled === 'true';
    if (rawConfig.web?.auth_mode === 'local') ldapOn = false;
    if (rawConfig.web?.auth_mode === 'ldap') ldapOn = true;

    $('#auth-mode').value = ldapOn ? 'ldap' : 'local';
    updateLDAPVisibility(ldapOn);

    $('#session-secret').value = rawConfig.web?.password || '';

    $('#ldap-host').value = rawConfig.ldap?.host || '';
    $('#ldap-bind-dn').value = rawConfig.ldap?.bind_dn || '';
    $('#ldap-bind-password').value = rawConfig.ldap?.bind_password || '';
    $('#ldap-base-dn').value = rawConfig.ldap?.base_dn || '';
    $('#ldap-filter').value = rawConfig.ldap?.filter || '';
    $('#ldap-username-attr').value = rawConfig.ldap?.username_attribute || '';

    $('#ldap-group-mode').value = rawConfig.ldap?.group_search_mode || '';
    $('#ldap-group-base-dn').value = rawConfig.ldap?.group_base_dn || '';
    $('#ldap-group-filter').value = rawConfig.ldap?.group_filter || '';
    $('#ldap-group-member-attr').value = rawConfig.ldap?.group_member_attribute || '';

    $('#wl-enabled').value = rawConfig.ldap?.whitelist_enabled ? 'true' : 'false';
    $('#sync-interval').value = rawConfig.ldap?.sync_interval || '24h';

    updateWhitelistVisibility();

    if (ldapOn && rawConfig.ldap?.whitelist_enabled) loadWhitelist();
  }

  function updateWhitelistVisibility() {
    var wlOn = $('#wl-enabled').value === 'true';
    $('#wl-section').classList.toggle('hidden', !wlOn);
  }

  function updateLDAPVisibility(ldapOn) {
    $('#ldap-section').style.display = ldapOn ? '' : 'none';
    $('#ldap-mode-notice').classList.toggle('hidden', !ldapOn);
  }

  async function saveAuthConfig() {
    showMsg('#auth-msg', '保存中...', true);
    var ldapOn = $('#auth-mode').value === 'ldap';
    if (!rawConfig.web) rawConfig.web = {};
    rawConfig.web.ldap_enabled = ldapOn;
    rawConfig.web.auth_mode = ldapOn ? 'ldap' : 'local';
    rawConfig.web.password = $('#session-secret').value;
    try {
      var res = await Api.post('/api/config', { config: JSON.stringify(rawConfig) });
      showMsg('#auth-msg', res.message || res.error, res.success);
    } catch (e) { showMsg('#auth-msg', e.message, false); }
  }

  async function saveLDAPConfig() {
    showMsg('#auth-msg', '保存中...', true);
    if (!rawConfig.ldap) rawConfig.ldap = {};
    rawConfig.ldap.host = $('#ldap-host').value;
    rawConfig.ldap.bind_dn = $('#ldap-bind-dn').value;
    rawConfig.ldap.bind_password = $('#ldap-bind-password').value;
    rawConfig.ldap.base_dn = $('#ldap-base-dn').value;
    rawConfig.ldap.filter = $('#ldap-filter').value;
    rawConfig.ldap.username_attribute = $('#ldap-username-attr').value;
    try {
      var res = await Api.post('/api/config', { config: JSON.stringify(rawConfig) });
      showMsg('#auth-msg', res.message || res.error, res.success);
    } catch (e) { showMsg('#auth-msg', e.message, false); }
  }

  async function saveGroupConfig() {
    showMsg('#sync-groups-msg', '保存中...', true);
    if (!rawConfig.ldap) rawConfig.ldap = {};
    rawConfig.ldap.group_search_mode = $('#ldap-group-mode').value;
    rawConfig.ldap.group_base_dn = $('#ldap-group-base-dn').value;
    rawConfig.ldap.group_filter = $('#ldap-group-filter').value;
    rawConfig.ldap.group_member_attribute = $('#ldap-group-member-attr').value;
    try {
      var res = await Api.post('/api/config', { config: JSON.stringify(rawConfig) });
      showMsg('#sync-groups-msg', res.message || res.error, res.success);
    } catch (e) { showMsg('#sync-groups-msg', e.message, false); }
  }

  async function saveSyncConfig() {
    showMsg('#auth-msg', '保存中...', true);
    if (!rawConfig.ldap) rawConfig.ldap = {};
    rawConfig.ldap.whitelist_enabled = $('#wl-enabled').value === 'true';
    rawConfig.ldap.sync_interval = $('#sync-interval').value;
    try {
      var res = await Api.post('/api/config', { config: JSON.stringify(rawConfig) });
      if (res.success) {
        showMsg('#auth-msg', res.message + '。自动同步间隔修改后需重启服务生效。', true);
      } else {
        showMsg('#auth-msg', res.error, false);
      }
      updateWhitelistVisibility();
      if (rawConfig.ldap.whitelist_enabled) loadWhitelist();
    } catch (e) { showMsg('#auth-msg', e.message, false); }
  }

  async function syncLDAPGroups() {
    showMsg('#sync-groups-msg', '同步中...', true);
    try {
      var res = await Api.post('/api/admin/auth/sync-groups', {});
      showMsg('#sync-groups-msg', res.message || res.error, res.success);
    } catch (e) { showMsg('#sync-groups-msg', e.message, false); }
  }

  async function testLDAP() {
    showMsg('#auth-msg', '测试连接中...', true);
    var resultDiv = $('#ldap-test-result');
    resultDiv.classList.add('hidden');
    resultDiv.innerHTML = '';
    try {
      var res = await Api.post('/api/admin/auth/test-ldap', {
        host: $('#ldap-host').value,
        bind_dn: $('#ldap-bind-dn').value,
        bind_password: $('#ldap-bind-password').value,
        base_dn: $('#ldap-base-dn').value,
        filter: $('#ldap-filter').value,
        username_attribute: $('#ldap-username-attr').value,
        group_search_mode: $('#ldap-group-mode').value,
        group_base_dn: $('#ldap-group-base-dn').value,
        group_filter: $('#ldap-group-filter').value,
        group_member_attribute: $('#ldap-group-member-attr').value
      });
      if (res.success) {
        showMsg('#auth-msg', '', true);
        var html = '<div style="color:var(--ok)">✓ 连接成功，找到 ' + res.user_count + ' 个用户</div>';
        var users = res.users || [];
        var userPreview = users.slice(0, 5).join(', ');
        if (res.user_count > 5) userPreview += '...';
        html += '<div style="margin-left:1em;color:var(--muted)">前 5 个用户: ' + ctx.esc(userPreview) + '</div>';

        var groups = res.groups || [];
        if (groups.length > 0) {
          html += '<div style="color:var(--ok);margin-top:0.5em">✓ 找到 ' + groups.length + ' 个组</div>';
          var groupPreview = groups.slice(0, 5).map(function(g) {
            var info = ctx.esc(g.name) + ' (' + g.member_count + '人';
            if (g.sub_groups && g.sub_groups.length > 0) info += ', 含 ' + g.sub_groups.length + ' 个子组';
            info += ')';
            return info;
          }).join(', ');
          if (groups.length > 5) groupPreview += '...';
          html += '<div style="margin-left:1em;color:var(--muted)">' + groupPreview + '</div>';
        } else if (res.group_error) {
          html += '<div style="color:var(--warn);margin-top:0.5em">⚠ 组查询失败: ' + ctx.esc(res.group_error) + '</div>';
        }
        resultDiv.innerHTML = html;
        resultDiv.classList.remove('hidden');
      } else {
        showMsg('#auth-msg', res.error, false);
      }
    } catch (e) { showMsg('#auth-msg', e.message, false); }
  }

  async function loadWhitelist() {
    var tags = $('#wl-tags');
    tags.innerHTML = '';
    var data = await Api.get('/api/admin/whitelist');
    if (!data.success) return;

    var users = data.users || [];
    if (users.length === 0) {
      tags.innerHTML = '<small class="text-muted">白名单为空</small>';
      return;
    }

    for (var i = 0; i < users.length; i++) {
      var tag = document.createElement('span');
      tag.className = 'tag';
      tag.innerHTML = ctx.esc(users[i]) + ' <button data-remove="' + ctx.esc(users[i]) + '">&times;</button>';
      tags.appendChild(tag);
    }

    tags.querySelectorAll('[data-remove]').forEach(function(btn) {
      btn.addEventListener('click', function() { removeWhitelistUser(btn.dataset.remove); });
    });
  }

  async function removeWhitelistUser(username) {
    if (!confirm('移除 ' + username + '？')) return;
    var data = await Api.get('/api/admin/whitelist');
    var users = (data.users || []).filter(function(u) { return u !== username; });
    var res = await Api.post('/api/admin/whitelist', { users: users.join(',') });
    showMsg('#wl-msg', res.message || res.error, res.success);
    if (res.success) loadWhitelist();
  }

  async function addWhitelistUser() {
    var input = $('#wl-add-input');
    var newUsers = input.value.split(',').map(function(s) { return s.trim(); }).filter(Boolean);
    if (newUsers.length === 0) return;

    var data = await Api.get('/api/admin/whitelist');
    var existing = data.users || [];
    var merged = existing.concat(newUsers);
    merged = merged.filter(function(v, i, a) { return a.indexOf(v) === i; });

    var res = await Api.post('/api/admin/whitelist', { users: merged.join(',') });
    showMsg('#wl-msg', res.message || res.error, res.success);
    if (res.success) { input.value = ''; loadWhitelist(); }
  }

  async function searchLDAPUsers() {
    var query = $('#wl-search-input').value.trim().toLowerCase();
    var container = $('#wl-search-results');
    container.classList.remove('hidden');
    container.innerHTML = '<small>搜索中...</small>';

    try {
      var data = await Api.get('/api/admin/auth/ldap-users');
      if (!data.success) { container.innerHTML = '<small>' + ctx.esc(data.error) + '</small>'; return; }

      var users = data.users || [];
      if (query) {
        users = users.filter(function(u) { return u.toLowerCase().indexOf(query) !== -1; });
      }

      if (users.length === 0) {
        container.innerHTML = '<small>未找到用户</small>';
        return;
      }

      var wlData = await Api.get('/api/admin/whitelist');
      var wlUsers = wlData.users || [];

      container.innerHTML = '';
      var list = document.createElement('div');
      list.className = 'row';
      list.style.flexWrap = 'wrap';
      list.style.gap = '4px';

      users.slice(0, 50).forEach(function(u) {
        var inWhitelist = wlUsers.indexOf(u) !== -1;
        var tag = document.createElement('span');
        tag.className = 'tag' + (inWhitelist ? '' : ' tag-muted');
        tag.innerHTML = ctx.esc(u) + (inWhitelist ? ' <small>✓</small>' : ' <button data-wl-add="' + ctx.esc(u) + '">+</button>');
        list.appendChild(tag);
      });

      if (users.length > 50) {
        var more = document.createElement('small');
        more.textContent = '还有 ' + (users.length - 50) + ' 个用户...';
        list.appendChild(more);
      }

      container.appendChild(list);

      container.querySelectorAll('[data-wl-add]').forEach(function(btn) {
        btn.addEventListener('click', async function() {
          var username = btn.dataset.wlAdd;
          var wlD = await Api.get('/api/admin/whitelist');
          var ex = wlD.users || [];
          ex.push(username);
          var r = await Api.post('/api/admin/whitelist', { users: ex.join(',') });
          showMsg('#wl-msg', r.message || r.error, r.success);
          if (r.success) { loadWhitelist(); searchLDAPUsers(); }
        });
      });

    } catch (e) {
      container.innerHTML = '<small>' + ctx.esc(e.message) + '</small>';
    }
  }
}

async function getServerUrl() {
  var r = await chrome.storage.local.get('serverUrl');
  return (r.serverUrl || '').replace(/\/+$/, '');
}
