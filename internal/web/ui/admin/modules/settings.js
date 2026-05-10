var rawConfig = {};

export async function init(ctx) {
  const { Api, showMsg, $, $$ } = ctx;
  await loadConfig();
  await loadMigrationRulesInfo();

  $('#save-btn').addEventListener('click', saveConfig);
  $('#reset-btn').addEventListener('click', () => { if (confirm('重新加载？未保存的修改将丢失。')) loadConfig(); });
  $('#refresh-migration-rules-btn')?.addEventListener('click', refreshMigrationRules);
  $('#upload-migration-rules-btn')?.addEventListener('click', uploadMigrationRules);
  $('#migration-rules-file')?.addEventListener('change', function() {
    var file = this.files && this.files[0];
    $('#migration-rules-file-name').textContent = file ? file.name : '未选择适配包';
    $('#upload-migration-rules-btn').disabled = !file;
  });

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

  async function refreshMigrationRules() {
    if (!confirm('确定从远端更新 Picoclaw 配置适配包吗？')) return;
    showMsg('#settings-msg', '正在更新配置适配...', true);
    try {
      var res = await Api.post('/api/admin/migration-rules/refresh', {});
      showMsg('#settings-msg', res.message || res.error, res.success);
      if (res.success && res.rules) renderMigrationRulesInfo(res.rules);
    } catch (e) { showMsg('#settings-msg', e.message, false); }
  }

  async function loadMigrationRulesInfo() {
    try {
      var res = await Api.get('/api/admin/migration-rules');
      if (res.success && res.rules) renderMigrationRulesInfo(res.rules);
    } catch (e) {
      var el = $('#migration-rules-info');
      if (el) el.textContent = '配置适配读取失败: ' + e.message;
    }
  }

  async function uploadMigrationRules() {
    var fileInput = $('#migration-rules-file');
    var file = fileInput && fileInput.files && fileInput.files[0];
    if (!file) {
      showMsg('#settings-msg', '请选择配置适配 zip 包', false);
      return;
    }
    showMsg('#settings-msg', '正在导入配置适配包...', true);
    try {
      var csrf = await getCSRF();
      var form = new FormData();
      form.append('file', file);
      form.append('csrf_token', csrf);
      var base = await getServerUrl();
      var resp = await fetch(base + '/api/admin/migration-rules/upload', {
        method: 'POST',
        credentials: 'include',
        body: form,
      });
      var res = await resp.json();
      showMsg('#settings-msg', res.message || res.error, !!res.success);
      if (res.success && res.rules) renderMigrationRulesInfo(res.rules);
      if (fileInput && res.success) {
        fileInput.value = '';
        $('#migration-rules-file-name').textContent = '未选择适配包';
        $('#upload-migration-rules-btn').disabled = true;
      }
    } catch (e) { showMsg('#settings-msg', e.message, false); }
  }

  function renderMigrationRulesInfo(rules) {
    var el = $('#migration-rules-info');
    if (!el || !rules) return;
    var versions = rules.versions || [];
    var changed = versions.filter(function(v) { return !!v.config_changed; }).map(function(v) {
      return v.version + '(' + v.from_config + '->' + v.to_config + ')';
    });
    var versionText = versions.map(function(v) {
      return v.version + ':v' + v.config_version + (v.config_changed ? '*' : '');
    }).join('，');
    el.innerHTML =
      (rules.adapter_version ? 'Adapter ' + ctx.esc(rules.adapter_version) + '，' : '') +
      (rules.adapter_schema_version ? '规则格式 v' + ctx.esc(rules.adapter_schema_version) + '，' : '') +
      '当前支持配置版本 v' + ctx.esc(rules.picoaide_supported_config_version) +
      '，规则声明最高 v' + ctx.esc(rules.latest_supported_config_version) +
      (rules.updated_at ? '，更新时间 ' + ctx.esc(rules.updated_at) : '') +
      '<br>版本映射：' + ctx.esc(versionText || '无') +
      '<br>配置变更：' + ctx.esc(changed.join('，') || '无');
  }

  $('#apply-all-btn').addEventListener('click', async function() {
    if (!confirm('确定要下发配置到所有用户并重启其容器吗？')) return;
    showMsg('#settings-msg', '正在提交下发任务...', true);
    try {
      var res = await Api.post('/api/admin/config/apply', {});
      if (res.task_id) {
        pollTaskStatus('#settings-msg', res.message);
      } else {
        showMsg('#settings-msg', res.message || res.error, res.success);
      }
    } catch (e) { showMsg('#settings-msg', e.message, false); }
  });

  function collectFields() {
    $$('input[data-path]').forEach(function(input) {
      deepSet(rawConfig, input.dataset.path, input.value);
    });
    $$('select[data-path]').forEach(function(sel) {
      deepSet(rawConfig, sel.dataset.path, sel.value);
    });
  }

  function removeFixedConfigFields() {
    if (rawConfig.web) delete rawConfig.web.container_base_url;
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

async function pollTaskStatus(selector, initialMsg) {
  showMsg(selector, initialMsg + '...', true);
  var poll = async function() {
    try {
      var st = await Api.get('/api/admin/task/status');
      if (st.running) {
        var pct = st.total > 0 ? Math.round((st.done + st.failed) / st.total * 100) : 0;
        showMsg(selector, initialMsg + ' ' + (st.done + st.failed) + '/' + st.total + ' (' + pct + '%)', true);
        setTimeout(poll, 2000);
      } else {
        showMsg(selector, st.message || '完成', st.failed === 0);
      }
    } catch (e) {
      showMsg(selector, '查询进度失败: ' + e.message, false);
    }
  };
  setTimeout(poll, 1500);
}
