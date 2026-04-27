var rawConfig = {};

export async function init(ctx) {
  const { Api, showMsg, $, $$ } = ctx;
  await loadConfig();

  $('#save-btn').addEventListener('click', saveConfig);
  $('#reset-btn').addEventListener('click', () => { if (confirm('重新加载？未保存的修改将丢失。')) loadConfig(); });
  $('#ldap-enabled').addEventListener('change', () => {
    var checked = $('#ldap-enabled').checked;
    $('#ldap-section').style.display = checked ? '' : 'none';
    if (!rawConfig.web) rawConfig.web = {};
    rawConfig.web.ldap_enabled = checked;
    rawConfig.web.auth_mode = checked ? 'ldap' : 'local';
  });
  $('#add-model-btn').addEventListener('click', () => {
    if (!rawConfig.picoclaw) rawConfig.picoclaw = {};
    if (!rawConfig.picoclaw.model_list) rawConfig.picoclaw.model_list = [];
    rawConfig.picoclaw.model_list.push({ model_name: '', model: '', api_base: '', request_timeout: 6000 });
    renderModelList();
  });
  $('#add-security-key-btn').addEventListener('click', () => {
    if (!rawConfig.security) rawConfig.security = {};
    if (!rawConfig.security.model_list) rawConfig.security.model_list = {};
    var key = 'new-model:0', n = 1;
    while (rawConfig.security.model_list[key]) key = 'new-model-' + n++ + ':0';
    rawConfig.security.model_list[key] = { api_keys: [''] };
    renderSecurityKeys();
  });

  async function loadConfig() {
    showMsg('#settings-msg', '加载中...', true);
    try {
      var base = await getServerUrl();
      var resp = await fetch(base + '/api/config', { method: 'GET', credentials: 'include' });
      var text = await resp.text();
      try { var e = JSON.parse(text); if (e.success === false) { showMsg('#settings-msg', e.error, false); return; } } catch {}
      rawConfig = JSON.parse(text);
      showMsg('#settings-msg', '');
      renderConfig();
    } catch (e) { showMsg('#settings-msg', e.message, false); }
  }

  function renderConfig() {
    $$('input[data-path]').forEach(function(input) {
      var val = deepGet(rawConfig, input.dataset.path);
      if (val !== undefined && val !== null) input.value = val;
    });
    var ldapOn = rawConfig.web?.ldap_enabled !== false;
    // 支持 auth_mode 字段：如果 auth_mode 为 "local"，视为 LDAP 关闭
    if (rawConfig.web?.auth_mode === 'local') ldapOn = false;
    if (rawConfig.web?.auth_mode === 'ldap') ldapOn = true;
    $('#ldap-enabled').checked = ldapOn;
    $('#ldap-section').style.display = ldapOn ? '' : 'none';
    renderModelList();
    renderSecurityKeys();
  }

  function renderModelList() {
    var container = $('#model-list');
    container.innerHTML = '';
    var models = (rawConfig.picoclaw && rawConfig.picoclaw.model_list) || [];
    models.forEach(function(m, i) {
      var card = document.createElement('div');
      card.className = 'card';
      card.innerHTML = '<div class="grid-2"><div class="field"><label>模型名</label><input type="text" value="' + ctx.esc(m.model_name || '') + '" data-mi="' + i + '" data-mf="model_name"></div><div class="field"><label>API 模型 ID</label><input type="text" value="' + ctx.esc(m.model || '') + '" data-mi="' + i + '" data-mf="model"></div></div><div class="grid-2"><div class="field"><label>API Base</label><input type="text" value="' + ctx.esc(m.api_base || '') + '" data-mi="' + i + '" data-mf="api_base"></div><div class="field"><label>超时(秒)</label><input type="number" value="' + (m.request_timeout || '') + '" data-mi="' + i + '" data-mf="request_timeout"></div></div><div class="card-footer"><button class="btn btn-sm btn-danger" data-rm-model="' + i + '">删除</button></div>';
      container.appendChild(card);
    });
    container.querySelectorAll('[data-mf]').forEach(function(input) {
      input.addEventListener('change', function() {
        var idx = parseInt(input.dataset.mi);
        var val = input.value;
        if (input.dataset.mf === 'request_timeout' && val !== '') val = Number(val);
        if (!rawConfig.picoclaw) rawConfig.picoclaw = {};
        if (!rawConfig.picoclaw.model_list) rawConfig.picoclaw.model_list = [];
        rawConfig.picoclaw.model_list[idx][input.dataset.mf] = val;
      });
    });
    container.querySelectorAll('[data-rm-model]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        rawConfig.picoclaw.model_list.splice(parseInt(btn.dataset.rmModel), 1);
        renderModelList();
      });
    });
  }

  function renderSecurityKeys() {
    var container = $('#security-keys-list');
    container.innerHTML = '';
    var modelList = (rawConfig.security && rawConfig.security.model_list) || {};
    Object.entries(modelList).forEach(function(entry, idx) {
      var key = entry[0], cfg = entry[1];
      var keys = Array.isArray(cfg.api_keys) ? cfg.api_keys : [];
      var card = document.createElement('div');
      card.className = 'card';
      card.innerHTML = '<div class="row"><div class="field" style="flex:2"><label>模型标识</label><input type="text" value="' + ctx.esc(key) + '" data-si="' + idx + '" data-sf="key"></div><button class="btn btn-sm btn-danger" data-rm-sec="' + idx + '" style="align-self:flex-end">删除组</button></div><div id="sec-keys-' + idx + '" class="mt-1"></div><div class="card-footer"><button class="btn btn-sm btn-outline" data-add-key="' + idx + '">+ 添加密钥</button></div>';
      container.appendChild(card);

      var keysDiv = card.querySelector('#sec-keys-' + idx);
      keys.forEach(function(k, ki) {
        var row = document.createElement('div');
        row.className = 'row';
        row.innerHTML = '<input type="password" value="' + ctx.esc(k) + '" data-si="' + idx + '" data-ki="' + ki + '" data-sf="keyval"><button class="btn btn-sm btn-outline" data-rm-key="' + idx + ':' + ki + '">&times;</button>';
        keysDiv.appendChild(row);
      });
    });
    container.querySelectorAll('[data-sf="key"]').forEach(function(input) {
      input.addEventListener('change', function() {
        var idx = parseInt(input.dataset.si);
        var names = Object.keys(modelList);
        var old = names[idx];
        if (input.value !== old) { modelList[input.value] = modelList[old]; delete modelList[old]; }
      });
    });
    container.querySelectorAll('[data-sf="keyval"]').forEach(function(input) {
      input.addEventListener('change', function() {
        var sIdx = parseInt(input.dataset.si);
        var kIdx = parseInt(input.dataset.ki);
        var names = Object.keys(modelList);
        modelList[names[sIdx]].api_keys[kIdx] = input.value;
      });
    });
    container.querySelectorAll('[data-rm-sec]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        var names = Object.keys(modelList);
        delete modelList[names[parseInt(btn.dataset.rmSec)]];
        renderSecurityKeys();
      });
    });
    container.querySelectorAll('[data-rm-key]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        var parts = btn.dataset.rmKey.split(':').map(Number);
        var names = Object.keys(modelList);
        modelList[names[parts[0]]].api_keys.splice(parts[1], 1);
        renderSecurityKeys();
      });
    });
    container.querySelectorAll('[data-add-key]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        var sIdx = parseInt(btn.dataset.addKey);
        var names = Object.keys(modelList);
        if (!modelList[names[sIdx]].api_keys) modelList[names[sIdx]].api_keys = [];
        modelList[names[sIdx]].api_keys.push('');
        renderSecurityKeys();
      });
    });
  }

  async function saveConfig() {
    showMsg('#settings-msg', '保存中...', true);
    collectFields();
    try {
      var res = await Api.post('/api/config', { config: JSON.stringify(rawConfig) });
      showMsg('#settings-msg', res.message || res.error, res.success);
    } catch (e) { showMsg('#settings-msg', e.message, false); }
  }

  function collectFields() {
    $$('input[data-path]').forEach(function(input) {
      deepSet(rawConfig, input.dataset.path, input.value);
    });
  }
}

function deepGet(obj, path) {
  var keys = path.split('.');
  var cur = obj;
  for (var i = 0; i < keys.length; i++) { if (cur == null) return undefined; cur = cur[keys[i]]; }
  return cur;
}

function deepSet(obj, path, value) {
  var keys = path.split('.');
  var cur = obj;
  for (var i = 0; i < keys.length - 1; i++) { if (cur[keys[i]] == null) cur[keys[i]] = {}; cur = cur[keys[i]]; }
  var last = keys[keys.length - 1];
  if (value !== '' && !isNaN(value) && String(Number(value)) === value) cur[last] = Number(value);
  else cur[last] = value;
}

async function getServerUrl() {
  var r = await chrome.storage.local.get('serverUrl');
  return (r.serverUrl || '').replace(/\/+$/, '');
}
