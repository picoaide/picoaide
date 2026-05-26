var rawConfig = {};

export async function init(ctx) {
  const { Api, showMsg, $, $$ } = ctx;
  await loadConfig();

  $('#save-btn').addEventListener('click', saveConfig);
  $('#reset-btn').addEventListener('click', async () => { if (await confirmModal('重新加载？未保存的修改将丢失。')) loadConfig(); });

  async function loadConfig() {
    showMsg('#settings-msg', '加载中...', true);
    try {
      var base = await getServerUrl();
      var resp = await fetch(base + '/api/config', { method: 'GET', credentials: 'include' });
      var text = await resp.text();
      try { var e = JSON.parse(text); if (e.success === false) { showMsg('#settings-msg', e.error, false); return; } } catch {}
      rawConfig = JSON.parse(text);
      removeFixedConfigFields();
      showMsg('#settings-msg', '');
      renderConfig();
    } catch (e) { showMsg('#settings-msg', e.message, false); }
  }

  function renderConfig() {
    $$('input[data-path]').forEach(function(input) {
      var val = deepGet(rawConfig, input.dataset.path);
      if (val !== undefined && val !== null) input.value = val;
    });
    $$('select[data-path]').forEach(function(sel) {
      var val = deepGet(rawConfig, sel.dataset.path);
      if (val !== undefined && val !== null) sel.value = val;
    });
    // 调试模式开关
    var debugMode = deepGet(rawConfig, 'web.debug_mode');
    if ($('#debug-mode-toggle')) {
      $('#debug-mode-toggle').checked = debugMode === true;
    }
  }

  async function saveConfig() {
    showMsg('#settings-msg', '保存中...', true);
    collectFields();
    removeFixedConfigFields();
    try {
      var res = await Api.post('/api/config', { config: JSON.stringify(rawConfig) });
      showMsg('#settings-msg', res.message || res.error, res.success);
    } catch (e) { showMsg('#settings-msg', e.message, false); }
  }

  // 技能市场开关
  await loadSkillPolicy();
  $('#skill-install-toggle')?.addEventListener('change', async function() {
    var disabled = !this.checked;
    showMsg('#settings-msg', '保存中...', true);
    try {
      var res = await Api.post('/api/admin/skill-install-policy', { disabled: String(disabled) });
      showMsg('#settings-msg', res.message || (res.success ? (disabled ? '已禁止市场安装' : '已允许市场安装') : res.error), res.success);
    } catch (e) {
      this.checked = !this.checked;
      showMsg('#settings-msg', e.message, false);
    }
  });

  async function loadSkillPolicy() {
    try {
      var res = await Api.get('/api/admin/skill-install-policy');
      if (res.disabled !== undefined) {
        $('#skill-install-toggle').checked = !res.disabled;
      }
    } catch (e) {
      var el = $('#skill-install-toggle');
      if (el) el.disabled = true;
    }
  }

  function collectFields() {
    $$('input[data-path]').forEach(function(input) {
      deepSet(rawConfig, input.dataset.path, input.value);
    });
    $$('select[data-path]').forEach(function(sel) {
      deepSet(rawConfig, sel.dataset.path, sel.value);
    });
    // 调试模式开关
    if ($('#debug-mode-toggle')) {
      deepSet(rawConfig, 'web.debug_mode', $('#debug-mode-toggle').checked);
    }
  }

  function removeFixedConfigFields() {
    if (rawConfig.web) {
      delete rawConfig.web.password;
    }
  }
}

function deepGet(obj, path) {
  var keys = path.split('.');
  var cur = obj;
  for (var i = 0; i < keys.length; i++) {
    if (cur == null) return undefined;
    var key = keys[i];
    if (key === '__proto__' || key === 'constructor' || key === 'prototype') return undefined;
    cur = cur[key];
  }
  return cur;
}

function deepSet(obj, path, value) {
  var keys = path.split('.');
  var cur = obj;
  for (var i = 0; i < keys.length - 1; i++) {
    var key = keys[i];
    if (key === '__proto__' || key === 'constructor' || key === 'prototype') return;
    if (cur[key] == null) cur[key] = {};
    cur = cur[key];
  }
  var last = keys[keys.length - 1];
  if (last === '__proto__' || last === 'constructor' || last === 'prototype') return;
  if (value !== '' && !isNaN(value) && String(Number(value)) === value) cur[last] = Number(value);
  else cur[last] = value;
}

async function getServerUrl() {
  return window.location.origin.replace(/\/+$/, '');
}
