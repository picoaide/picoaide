var rawConfig = {};
var whitelistLoadSeq = 0;

export async function init(ctx) {
  const { Api, showMsg, $ } = ctx;
  await loadConfig();

  $('#auth-mode').addEventListener('change', function() {
    var mode = this.value;
    var current = currentAuthMode();
    if (mode !== current) {
      if (!confirm('切换认证方式会清空当前普通用户、容器记录、用户目录和归档目录。确定切换到 ' + authModeLabel(mode) + '？')) {
        this.value = current;
        return;
      }
    }
    updateProviderVisibility(mode);
  });

  $('#save-auth-config-btn').addEventListener('click', saveAllConfig);
  $('#save-auth-config-bottom-btn').addEventListener('click', saveAllConfig);
  $('#test-ldap-btn').addEventListener('click', testLDAP);
  $('#sync-users-btn').addEventListener('click', syncLDAPUsers);
  $('#sync-users-cleanup-btn').addEventListener('click', syncLDAPUsersWithCleanup);
  $('#sync-groups-btn').addEventListener('click', syncLDAPGroups);
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
    var mode = currentAuthMode();

    $('#auth-mode').value = mode;
    updateProviderVisibility(mode);

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

    $('#wl-enabled').value = whitelistEnabledForMode(mode) ? 'true' : 'false';
    $('#sync-interval').value = rawConfig.ldap?.sync_interval || '24h';
    $('#oidc-issuer-url').value = rawConfig.oidc?.issuer_url || '';
    $('#oidc-client-id').value = rawConfig.oidc?.client_id || '';
    $('#oidc-client-secret').value = rawConfig.oidc?.client_secret || '';
    $('#oidc-redirect-url').value = rawConfig.oidc?.redirect_url || '';
    $('#oidc-scopes').value = rawConfig.oidc?.scopes || 'openid profile email';
    $('#oidc-username-claim').value = rawConfig.oidc?.username_claim || 'preferred_username';
    $('#oidc-groups-claim').value = rawConfig.oidc?.groups_claim || 'groups';
    $('#oidc-sync-interval').value = rawConfig.oidc?.sync_interval || '0';

    updateWhitelistVisibility();

    if (mode !== 'local' && whitelistEnabledForMode(mode)) loadWhitelist();
  }

  function updateWhitelistVisibility() {
    var mode = $('#auth-mode').value;
    $('#auth-whitelist-section').style.display = mode === 'local' ? 'none' : '';
    $('#wl-search-row').style.display = mode === 'ldap' ? '' : 'none';
    $('#wl-add-input').placeholder = mode === 'oidc' ? '手动输入 OIDC 用户名' : '手动输入用户名';
    var wlOn = $('#wl-enabled').value === 'true';
    $('#wl-section').classList.toggle('hidden', mode === 'local' || !wlOn);
  }

  function whitelistEnabledForMode(mode) {
    if (mode === 'ldap') return rawConfig.ldap?.whitelist_enabled === true;
    if (mode === 'oidc') return rawConfig.oidc?.whitelist_enabled === true;
    return false;
  }

  function currentAuthMode() {
    if (rawConfig.web?.auth_mode) return rawConfig.web.auth_mode;
    var ldapOn = rawConfig.web?.ldap_enabled === true || rawConfig.web?.ldap_enabled === 'true';
    return ldapOn ? 'ldap' : 'local';
  }

  function authModeLabel(mode) {
    if (mode === 'ldap') return 'LDAP 统一登录';
    if (mode === 'oidc') return 'OIDC 统一登录';
    return '本地认证';
  }

  function updateProviderVisibility(mode) {
    $('#ldap-section').style.display = mode === 'ldap' ? '' : 'none';
    $('#oidc-section').style.display = mode === 'oidc' ? '' : 'none';
    $('#provider-mode-notice').classList.toggle('hidden', mode === 'local');
    if (mode !== 'local') {
      $('#wl-enabled').value = whitelistEnabledForMode(mode) ? 'true' : 'false';
    }
    updateWhitelistVisibility();
  }

  function collectConfigFromForm() {
    var mode = $('#auth-mode').value;
    if (!rawConfig.web) rawConfig.web = {};
    rawConfig.web.ldap_enabled = mode === 'ldap';
    rawConfig.web.auth_mode = mode;
    delete rawConfig.web.password;

    if (!rawConfig.ldap) rawConfig.ldap = {};
    rawConfig.ldap.host = $('#ldap-host').value;
    rawConfig.ldap.bind_dn = $('#ldap-bind-dn').value;
    rawConfig.ldap.bind_password = $('#ldap-bind-password').value;
    rawConfig.ldap.base_dn = $('#ldap-base-dn').value;
    rawConfig.ldap.filter = $('#ldap-filter').value;
    rawConfig.ldap.username_attribute = $('#ldap-username-attr').value;

    rawConfig.ldap.group_search_mode = $('#ldap-group-mode').value;
    rawConfig.ldap.group_base_dn = $('#ldap-group-base-dn').value;
    rawConfig.ldap.group_filter = $('#ldap-group-filter').value;
    rawConfig.ldap.group_member_attribute = $('#ldap-group-member-attr').value;

    rawConfig.ldap.whitelist_enabled = mode === 'ldap' ? $('#wl-enabled').value === 'true' : rawConfig.ldap.whitelist_enabled === true;
    rawConfig.ldap.sync_interval = $('#sync-interval').value;

    if (!rawConfig.oidc) rawConfig.oidc = {};
    rawConfig.oidc.issuer_url = $('#oidc-issuer-url').value;
    rawConfig.oidc.client_id = $('#oidc-client-id').value;
    rawConfig.oidc.client_secret = $('#oidc-client-secret').value;
    rawConfig.oidc.redirect_url = $('#oidc-redirect-url').value;
    rawConfig.oidc.scopes = $('#oidc-scopes').value;
    rawConfig.oidc.username_claim = $('#oidc-username-claim').value;
    rawConfig.oidc.groups_claim = $('#oidc-groups-claim').value;
    rawConfig.oidc.whitelist_enabled = mode === 'oidc' ? $('#wl-enabled').value === 'true' : rawConfig.oidc.whitelist_enabled === true;
    rawConfig.oidc.sync_interval = $('#oidc-sync-interval').value;
  }

  async function saveAllConfig() {
    showMsg('#auth-msg', '保存中...', true);
    collectConfigFromForm();
    try {
      var res = await Api.post('/api/config', { config: JSON.stringify(rawConfig) });
      if (res.success) {
        showMsg('#auth-msg', res.message + '。自动同步间隔修改后需重启服务生效。', true);
        await loadConfig();
      } else {
        showMsg('#auth-msg', res.error, false);
      }
      updateWhitelistVisibility();
    } catch (e) { showMsg('#auth-msg', e.message, false); }
  }

  async function syncLDAPUsers() {
    showMsg('#sync-groups-msg', '同步账号中...', true);
    try {
      var res = await Api.post('/api/admin/auth/sync-users', {});
      showMsg('#sync-groups-msg', res.message || res.error, res.success);
    } catch (e) { showMsg('#sync-groups-msg', e.message, false); }
  }

  async function syncLDAPUsersWithCleanup() {
    if (!confirm('确定同步账号并清理不在当前认证源或白名单中的旧账号吗？')) return;
    showMsg('#sync-groups-msg', '同步并清理旧账号中...', true);
    try {
      var res = await Api.post('/api/admin/auth/sync-users', { cleanup: 'true' });
      showMsg('#sync-groups-msg', res.message || res.error, res.success);
    } catch (e) { showMsg('#sync-groups-msg', e.message, false); }
  }

  async function syncLDAPGroups() {
    showMsg('#sync-groups-msg', '同步组中...', true);
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
    var seq = ++whitelistLoadSeq;
    var tags = $('#wl-tags');
    tags.innerHTML = '';
    var data = await Api.get('/api/admin/whitelist?page=1&page_size=100');
    if (seq !== whitelistLoadSeq) return;
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
    if ((data.total || users.length) > users.length) {
      var more = document.createElement('small');
      more.textContent = '已显示前 ' + users.length + ' 个，共 ' + data.total + ' 个；可在白名单管理页搜索和翻页。';
      tags.appendChild(more);
    }

    tags.querySelectorAll('[data-remove]').forEach(function(btn) {
      btn.addEventListener('click', function() { removeWhitelistUser(btn.dataset.remove); });
    });
  }

  async function removeWhitelistUser(username) {
    if (!confirm('移除 ' + username + '？')) return;
    var res = await Api.post('/api/admin/whitelist', { remove: username });
    showMsg('#wl-msg', res.message || res.error, res.success);
    if (res.success) loadWhitelist();
  }

  async function addWhitelistUser() {
    var input = $('#wl-add-input');
    var newUsers = input.value.split(',').map(function(s) { return s.trim(); }).filter(Boolean);
    if (newUsers.length === 0) return;

    var res = await Api.post('/api/admin/whitelist', { add: newUsers.join(',') });
    showMsg('#wl-msg', res.message || res.error, res.success);
    if (res.success) { input.value = ''; loadWhitelist(); }
  }

  async function searchLDAPUsers() {
    var query = $('#wl-search-input').value.trim().toLowerCase();
    var container = $('#wl-search-results');
    container.classList.remove('hidden');
    container.innerHTML = '<small>搜索中...</small>';

    try {
      var url = '/api/admin/auth/ldap-users?source=directory&page=1&page_size=50';
      if (query) url += '&search=' + encodeURIComponent(query);
      var data = await Api.get(url);
      if (!data.success) { container.innerHTML = '<small>' + ctx.esc(data.error) + '</small>'; return; }

      var users = data.users || [];

      if (users.length === 0) {
        container.innerHTML = '<small>未找到用户</small>';
        return;
      }

      var wlUrl = '/api/admin/whitelist?page=1&page_size=200';
      if (query) wlUrl += '&search=' + encodeURIComponent(query);
      var wlData = await Api.get(wlUrl);
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

      if ((data.total || users.length) > users.length) {
        var more = document.createElement('small');
        more.textContent = '还有 ' + ((data.total || users.length) - users.length) + ' 个用户，请输入更精确的关键字';
        list.appendChild(more);
      }

      container.appendChild(list);

      container.querySelectorAll('[data-wl-add]').forEach(function(btn) {
        btn.addEventListener('click', async function() {
          var username = btn.dataset.wlAdd;
          var r = await Api.post('/api/admin/whitelist', { add: username });
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
  return window.location.origin.replace(/\/+$/, '');
}
