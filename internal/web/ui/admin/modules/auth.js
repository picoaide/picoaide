var rawConfig = Object.create(null);
var providers = [];
var whitelistLoadSeq = 0;
var initialized = false;

export async function init(ctx) {
  const { Api, showMsg, $ } = ctx;
  await loadProviders(ctx);
  await loadConfig(ctx);
  initialized = true;

  $('#auth-mode').addEventListener('change', async function() {
    if (!initialized) return;
    var mode = this.value;
    var current = currentAuthMode();
    if (mode !== current) {
      this.blur(); // 关闭下拉选项避免遮盖模态框
      if (!await confirmModal('切换认证方式会清空当前普通用户、容器记录、用户目录和归档目录。确定切换到 ' + authModeLabel(mode) + '？')) {
        this.value = current;
        updateProviderVisibility(current);
        renderProviderConfig(ctx, current);
        updateWhitelistVisibility();
        return;
      }
    }
    updateProviderVisibility(mode);
    renderProviderConfig(ctx, mode);
    updateWhitelistVisibility();
  });

  $('#save-auth-config-btn').addEventListener('click', function() { saveAllConfig(ctx); });
  $('#save-auth-config-bottom-btn').addEventListener('click', function() { saveAllConfig(ctx); });
  $('#wl-enabled').addEventListener('change', updateWhitelistVisibility);
  $('#wl-add-btn').addEventListener('click', function() { addWhitelistUser(ctx); });
  $('#wl-search-btn').addEventListener('click', function() { searchLDAPUsers(ctx); });
}

// ============================================================
// 嵌套 Config 通用读取/写入
// ============================================================

function getConfigValue(key) {
  var parts = key.split('.');
  var current = rawConfig;
  for (var i = 0; i < parts.length; i++) {
    if (current == null || typeof current !== 'object') return undefined;
    current = current[parts[i]];
  }
  return current;
}

function setConfigValue(key, value) {
  if (key.indexOf('__proto__') >= 0) return;
  if (key === 'constructor' || key.indexOf('.constructor') >= 0) return;
  if (key === 'prototype' || key.indexOf('.prototype') >= 0) return;
  var parts = key.split('.');
  for (var i = 0; i < parts.length; i++) {
    if (parts[i] === '__proto__' || parts[i] === 'constructor' || parts[i] === 'prototype') {
      return;
    }
  }
  var current = rawConfig;
  for (var i = 0; i < parts.length - 1; i++) {
    var part = parts[i];
    if (typeof current[part] !== 'object' || current[part] === null) {
      current[part] = Object.create(null);
    }
    current = current[part];
  }
  var lastKey = parts[parts.length - 1];
  current[lastKey] = value;
}

// ============================================================
// Provider 定义加载
// ============================================================

async function loadProviders(ctx) {
  var data = await ctx.Api.get('/api/admin/auth/providers');
  if (!data || !data.success) return;
  providers = data.providers || [];
  var sel = document.getElementById('auth-mode');
  sel.innerHTML = '';
  providers.forEach(function(p) {
    var opt = document.createElement('option');
    opt.value = p.name;
    opt.textContent = p.display_name;
    sel.appendChild(opt);
  });
}

// ============================================================
// 配置加载
// ============================================================

async function loadConfig(ctx) {
  const { Api, showMsg } = ctx;
  showMsg('#auth-msg', '加载中...', true);
  try {
    var base = getServerUrl();
    var resp = await fetch(base + '/api/config', { method: 'GET', credentials: 'include' });
    var text = await resp.text();
    try { var e = JSON.parse(text); if (e.success === false) { showMsg('#auth-msg', e.error, false); return; } } catch {}
    rawConfig = JSON.parse(text);
    showMsg('#auth-msg', '');
    renderAll(ctx);
  } catch (e) { showMsg('#auth-msg', e.message, false); }
}

// ============================================================
// 渲染
// ============================================================

function renderAll(ctx) {
  var mode = currentAuthMode();
  var sel = document.getElementById('auth-mode');
  if (sel.value !== mode) sel.value = mode;
  updateProviderVisibility(mode);
  renderProviderConfig(ctx, mode);
  updateWhitelistVisibility();
  if (mode !== 'local' && whitelistEnabledForMode(mode)) loadWhitelist(ctx);
}

function renderProviderConfig(ctx, mode) {
  var container = document.getElementById('provider-config-container');
  container.innerHTML = '';
  var provider = providers.find(function(p) { return p.name === mode; });
  if (!provider || !provider.fields || provider.fields.length === 0) return;

  var providerActions = (provider.actions || []).slice();
  var hasDirectory = provider.has_directory;

  provider.fields.forEach(function(section) {
    var card = document.createElement('div');
    card.className = 'card';

    var header = document.createElement('div');
    header.className = 'card-header';
    header.textContent = section.name;
    card.appendChild(header);

    // 字段网格
    var grid = document.createElement('div');
    grid.className = 'grid-2';

    section.fields.forEach(function(field) {
      var fieldDiv = document.createElement('div');
      fieldDiv.className = 'field';
      var label = document.createElement('label');
      label.textContent = field.label;
      fieldDiv.appendChild(label);
      fieldDiv.appendChild(createFieldElement(field));
      grid.appendChild(fieldDiv);
    });

    card.appendChild(grid);

    // 当前 section 的操作按钮
    var sectionActions = providerActions.filter(function(a) { return a.section === section.name; });
    sectionActions.forEach(function(action) {
      var row = document.createElement('div');
      row.className = 'row mt-1';
      var btn = document.createElement('button');
      btn.className = 'btn btn-outline btn-sm';
      btn.id = 'action-' + action.id;
      btn.textContent = action.label;
      btn.addEventListener('click', function() { handleAction(ctx, action.id); });
      row.appendChild(btn);
      card.appendChild(row);
      // 结果展示区
      var resultDiv = document.createElement('div');
      resultDiv.id = action.id + '-result';
      resultDiv.className = 'mt-1 hidden';
      resultDiv.style.fontSize = '0.9em';
      card.appendChild(resultDiv);
    });

    // Directory 能力：在"同步" section 后追加同步按钮
    if (hasDirectory && section.fields.some(function(f) { return f.key.indexOf('sync_interval') >= 0; })) {
      var syncRow = document.createElement('div');
      syncRow.className = 'row mt-1';
      syncRow.appendChild(createSyncBtn('sync-users-btn', '同步账号（自动清理旧账号）', function() { syncDirectoryUsers(ctx); }));
      syncRow.appendChild(createSyncBtn('sync-groups-btn', '同步用户组', function() { syncDirectoryGroups(ctx); }));
      card.appendChild(syncRow);
      var msgDiv = document.createElement('div');
      msgDiv.id = 'sync-groups-msg';
      msgDiv.className = 'msg';
      card.appendChild(msgDiv);
    }

    container.appendChild(card);
  });

  // 回填已有配置值
  fillFieldValues();
}

function createFieldElement(field) {
  var el;
  if (field.type === 'select') {
    el = document.createElement('select');
    (field.options || []).forEach(function(opt) {
      var o = document.createElement('option');
      o.value = opt.value;
      o.textContent = opt.label;
      el.appendChild(o);
    });
  } else if (field.type === 'password') {
    el = document.createElement('input');
    el.type = 'password';
  } else {
    el = document.createElement('input');
    el.type = 'text';
  }
  el.dataset.fieldKey = field.key;
  if (field.placeholder) el.placeholder = field.placeholder;
  if (field.required) el.required = true;
  if (field.default) el.dataset.default = field.default;
  return el;
}

function createSyncBtn(id, label, handler) {
  var btn = document.createElement('button');
  btn.className = 'btn btn-outline btn-sm';
  btn.id = id;
  btn.textContent = label;
  btn.addEventListener('click', handler);
  return btn;
}

function fillFieldValues() {
  document.querySelectorAll('#provider-config-container [data-field-key]').forEach(function(el) {
    var key = el.dataset.fieldKey;
    var val = getConfigValue(key);
    if (val === undefined || val === null) {
      val = el.dataset.default || '';
    }
    el.value = String(val);
  });
}

// ============================================================
// 可见性控制
// ============================================================

function updateProviderVisibility(mode) {
  document.getElementById('provider-config-container').style.display = mode === 'local' ? 'none' : '';
  document.getElementById('provider-mode-notice').classList.toggle('hidden', mode === 'local');
  if (mode !== 'local') {
    var wlEnabled = getConfigValue(mode + '.whitelist_enabled') === true;
    document.getElementById('wl-enabled').value = wlEnabled ? 'true' : 'false';
  }
}

function updateWhitelistVisibility() {
  var mode = document.getElementById('auth-mode').value;
  document.getElementById('auth-whitelist-section').style.display = mode === 'local' ? 'none' : '';
  var provider = providers.find(function(p) { return p.name === mode; });
  document.getElementById('wl-search-row').style.display = provider && provider.has_directory ? '' : 'none';
  document.getElementById('wl-add-input').placeholder = mode === 'oidc' ? '手动输入 OIDC 用户名' : '手动输入用户名';
  var wlOn = document.getElementById('wl-enabled').value === 'true';
  document.getElementById('wl-section').classList.toggle('hidden', mode === 'local' || !wlOn);
}

function whitelistEnabledForMode(mode) {
  var val = getConfigValue(mode + '.whitelist_enabled');
  return val === true || val === 'true';
}

function currentAuthMode() {
  if (rawConfig.web?.auth_mode) return rawConfig.web.auth_mode;
  return 'local';
}

function authModeLabel(mode) {
  var p = providers.find(function(p) { return p.name === mode; });
  return p ? p.display_name : mode;
}

// ============================================================
// 保存配置
// ============================================================

function collectConfigFromForm() {
  var mode = document.getElementById('auth-mode').value;
  if (!rawConfig.web) rawConfig.web = {};
  rawConfig.web.auth_mode = mode;
  rawConfig.web.ldap_enabled = mode === 'ldap';
  delete rawConfig.web.password;

  // 收集动态字段值
  document.querySelectorAll('#provider-config-container [data-field-key]').forEach(function(el) {
    setConfigValue(el.dataset.fieldKey, el.value);
  });

  // 白名单开关写入当前 mode 对应的 key
  setConfigValue(mode + '.whitelist_enabled', document.getElementById('wl-enabled').value === 'true');
}

async function saveAllConfig(ctx) {
  const { Api, showMsg } = ctx;
  showMsg('#auth-msg', '保存中...', true);
  collectConfigFromForm();
  try {
    var res = await Api.post('/api/config', { config: JSON.stringify(rawConfig) });
    if (res.success) {
      showMsg('#auth-msg', res.message + '。自动同步间隔已实时生效。', true);
      await loadConfig(ctx);
    } else {
      showMsg('#auth-msg', res.error, false);
    }
    updateWhitelistVisibility();
  } catch (e) { showMsg('#auth-msg', e.message, false); }
}

// ============================================================
// 操作按钮
// ============================================================

async function handleAction(ctx, actionId) {
  switch (actionId) {
    case 'test-ldap':
      await testLDAP(ctx);
      break;
    default:
      ctx.showMsg('#auth-msg', '未知操作', false);
  }
}

async function testLDAP(ctx) {
  const { Api, showMsg } = ctx;
  showMsg('#auth-msg', '测试连接中...', true);
  var resultDiv = document.getElementById('test-ldap-result');
  if (resultDiv) {
    resultDiv.classList.add('hidden');
    resultDiv.innerHTML = '';
  }
  try {
    var res = await Api.post('/api/admin/auth/test-ldap', {
      host: getFormValue('ldap.host'),
      bind_dn: getFormValue('ldap.bind_dn'),
      bind_password: getFormValue('ldap.bind_password'),
      base_dn: getFormValue('ldap.base_dn'),
      filter: getFormValue('ldap.filter'),
      username_attribute: getFormValue('ldap.username_attribute'),
      group_search_mode: getFormValue('ldap.group_search_mode'),
      group_base_dn: getFormValue('ldap.group_base_dn'),
      group_filter: getFormValue('ldap.group_filter'),
      group_member_attribute: getFormValue('ldap.group_member_attribute')
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
      if (resultDiv) { resultDiv.innerHTML = html; resultDiv.classList.remove('hidden'); }
    } else {
      showMsg('#auth-msg', res.error, false);
    }
  } catch (e) { showMsg('#auth-msg', e.message, false); }
}

function getFormValue(key) {
  var el = document.querySelector('[data-field-key="' + key + '"]');
  return el ? el.value : '';
}

// ============================================================
// 同步操作
// ============================================================

async function syncDirectoryUsers(ctx) {
  const { Api, showMsg } = ctx;
  showMsg('#sync-groups-msg', '同步账号中...', true);
  try {
    var res = await Api.post('/api/admin/auth/sync-users', { cleanup: 'true' });
    showMsg('#sync-groups-msg', res.message || res.error, res.success);
  } catch (e) { showMsg('#sync-groups-msg', e.message, false); }
}

async function syncDirectoryGroups(ctx) {
  const { Api, showMsg } = ctx;
  showMsg('#sync-groups-msg', '同步组中...', true);
  try {
    var res = await Api.post('/api/admin/auth/sync-groups', {});
    showMsg('#sync-groups-msg', res.message || res.error, res.success);
  } catch (e) { showMsg('#sync-groups-msg', e.message, false); }
}

// ============================================================
// 白名单
// ============================================================

async function loadWhitelist(ctx) {
  const { Api, showMsg, $ } = ctx;
  var seq = ++whitelistLoadSeq;
  var tags = document.getElementById('wl-tags');
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
    btn.addEventListener('click', function() { removeWhitelistUser(ctx, btn.dataset.remove); });
  });
}

async function removeWhitelistUser(ctx, username) {
  const { Api, showMsg } = ctx;
  if (!await confirmModal('移除 ' + username + '？')) return;
  var res = await Api.post('/api/admin/whitelist', { remove: username });
  showMsg('#wl-msg', res.message || res.error, res.success);
  if (res.success) loadWhitelist(ctx);
}

async function addWhitelistUser(ctx) {
  const { Api, showMsg } = ctx;
  var input = document.getElementById('wl-add-input');
  var newUsers = input.value.split(',').map(function(s) { return s.trim(); }).filter(Boolean);
  if (newUsers.length === 0) return;

  var res = await Api.post('/api/admin/whitelist', { add: newUsers.join(',') });
  showMsg('#wl-msg', res.message || res.error, res.success);
  if (res.success) { input.value = ''; loadWhitelist(ctx); }
}

async function searchLDAPUsers(ctx) {
  const { Api, showMsg } = ctx;
  var query = document.getElementById('wl-search-input').value.trim().toLowerCase();
  var container = document.getElementById('wl-search-results');
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
        if (r.success) { loadWhitelist(ctx); searchLDAPUsers(ctx); }
      });
    });

  } catch (e) {
    container.innerHTML = '<small>' + ctx.esc(e.message) + '</small>';
  }
}

// ============================================================
// 工具
// ============================================================



function getServerUrl() {
  return window.location.origin.replace(/\/+$/, '');
}
