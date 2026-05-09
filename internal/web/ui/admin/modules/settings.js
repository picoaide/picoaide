var rawConfig = {};

var VENDORS = {
  'openai':       { label: 'OpenAI',        api_base: 'https://api.openai.com/v1' },
  'anthropic':    { label: 'Anthropic',      api_base: 'https://api.anthropic.com/v1' },
  'azure':        { label: 'Azure OpenAI',   api_base: '' },
  'deepseek':     { label: 'DeepSeek',       api_base: 'https://api.deepseek.com/v1' },
  'ollama':       { label: 'Ollama',         api_base: 'http://localhost:11434/v1' },
  'qwen':         { label: '通义千问',        api_base: 'https://dashscope.aliyuncs.com/compatible-mode/v1' },
  'volcengine':   { label: '火山引擎',        api_base: 'https://ark.cn-beijing.volces.com/api/v3' },
  'zhipu':        { label: '智谱',            api_base: 'https://open.bigmodel.cn/api/paas/v4' },
  'groq':         { label: 'Groq',           api_base: 'https://api.groq.com/openai/v1' },
  'moonshot':     { label: 'Moonshot',       api_base: 'https://api.moonshot.cn/v1' },
  'lmstudio':     { label: 'LM Studio',      api_base: 'http://localhost:1234/v1' },
  'openrouter':   { label: 'OpenRouter',     api_base: 'https://openrouter.ai/api/v1' },
  'gemini':       { label: 'Google Gemini',  api_base: '' },
  'custom':       { label: '自定义',          api_base: '' }
};

function parseModelField(model) {
  if (!model) return { vendor: 'custom', modelId: '' };
  var slash = model.indexOf('/');
  if (slash === -1) return { vendor: 'custom', modelId: model };
  var v = model.substring(0, slash);
  var id = model.substring(slash + 1);
  if (VENDORS[v]) return { vendor: v, modelId: id };
  return { vendor: 'custom', modelId: model };
}

function buildModelField(vendor, modelId) {
  if (vendor === 'custom') return modelId;
  return vendor + '/' + modelId;
}

export async function init(ctx) {
  const { Api, showMsg, $, $$ } = ctx;
  await loadConfig();

  $('#save-btn').addEventListener('click', saveConfig);
  $('#reset-btn').addEventListener('click', () => { if (confirm('重新加载？未保存的修改将丢失。')) loadConfig(); });
  $('#refresh-migration-rules-btn')?.addEventListener('click', refreshMigrationRules);
  $('#add-model-btn').addEventListener('click', () => {
    if (!rawConfig.picoclaw) rawConfig.picoclaw = {};
    if (!rawConfig.picoclaw.model_list) rawConfig.picoclaw.model_list = [];
    rawConfig.picoclaw.model_list.push({ model_name: '', model: '', api_base: '', request_timeout: 6000 });
    renderModelList();
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
    $$('select[data-path]').forEach(function(sel) {
      var val = deepGet(rawConfig, sel.dataset.path);
      if (val !== undefined && val !== null) sel.value = val;
    });
    renderModelList();
  }

  // 构建安全密钥的查找映射：model_name -> api_keys[]
  function getSecurityMap() {
    var secList = (rawConfig.security && rawConfig.security.model_list) || {};
    var map = {};
    Object.keys(secList).forEach(function(key) {
      // key 格式为 "model_name:0"，提取 model_name
      var parts = key.split(':');
      var modelName = parts.slice(0, -1).join(':');
      var keys = Array.isArray(secList[key].api_keys) ? secList[key].api_keys : [];
      if (!map[modelName]) map[modelName] = [];
      map[modelName] = map[modelName].concat(keys);
    });
    return map;
  }

  // 保存安全密钥映射回 security.model_list
  function setSecurityMap(secMap) {
    if (!rawConfig.security) rawConfig.security = {};
    rawConfig.security.model_list = {};
    Object.keys(secMap).forEach(function(modelName) {
      var keys = secMap[modelName];
      if (keys && keys.length > 0) {
        rawConfig.security.model_list[modelName + ':0'] = { api_keys: keys };
      }
    });
  }

  function renderModelList() {
    var container = $('#model-list');
    container.innerHTML = '';
    var models = (rawConfig.picoclaw && rawConfig.picoclaw.model_list) || [];
    var secMap = getSecurityMap();

    // 更新默认模型下拉
    var defaultSelect = $('#default-model-select');
    if (defaultSelect) {
      var currentDefault = deepGet(rawConfig, 'picoclaw.agents.defaults.model_name') || '';
      defaultSelect.innerHTML = '<option value="">-- 选择默认模型 --</option>';
      models.forEach(function(m) {
        var opt = document.createElement('option');
        opt.value = m.model_name || '';
        opt.textContent = m.model_name || '(未命名)';
        if (m.model_name === currentDefault) opt.selected = true;
        defaultSelect.appendChild(opt);
      });
    }

    models.forEach(function(m, i) {
      var keys = secMap[m.model_name] || [];
      var parsed = parseModelField(m.model);
      var card = document.createElement('div');
      card.className = 'card';
      card.style.marginBottom = '12px';

      var vendorOptions = '';
      Object.keys(VENDORS).forEach(function(vk) {
        vendorOptions += '<option value="' + vk + '"' + (parsed.vendor === vk ? ' selected' : '') + '>' + VENDORS[vk].label + '</option>';
      });

      var defaultBase = VENDORS[parsed.vendor] ? VENDORS[parsed.vendor].api_base : '';
      var apiBaseValue = m.api_base || defaultBase;

      var keysHtml = '';
      keys.forEach(function(k, ki) {
        keysHtml += '<div class="row" style="margin-top:4px">' +
          '<input type="password" value="' + ctx.esc(k) + '" data-mi="' + i + '" data-ki="' + ki + '" data-sf="keyval" style="flex:1">' +
          '<button class="btn btn-sm btn-outline" data-rm-key="' + i + ':' + ki + '">&times;</button>' +
          '</div>';
      });

      card.innerHTML =
        '<div class="grid-2">' +
          '<div class="field"><label>模型名</label><input type="text" value="' + ctx.esc(m.model_name || '') + '" data-mi="' + i + '" data-mf="model_name"></div>' +
          '<div class="field"><label>供应商 / 模型</label><div class="row">' +
            '<select data-mi="' + i + '" data-vf="vendor" style="min-width:120px">' + vendorOptions + '</select>' +
            '<input type="text" value="' + ctx.esc(parsed.modelId) + '" data-mi="' + i + '" data-vf="model_id" placeholder="模型 ID" style="flex:1">' +
          '</div></div>' +
        '</div>' +
        '<div class="grid-2">' +
          '<div class="field"><label>API Base</label><input type="text" value="' + ctx.esc(apiBaseValue) + '" data-mi="' + i + '" data-mf="api_base" placeholder="' + ctx.esc(defaultBase) + '"></div>' +
          '<div class="field"><label>超时(秒)</label><input type="number" value="' + (m.request_timeout || '') + '" data-mi="' + i + '" data-mf="request_timeout"></div>' +
        '</div>' +
        '<div class="field"><label>API 密钥</label>' + keysHtml +
          '<button class="btn btn-sm btn-outline mt-1" data-add-key="' + i + '">+ 添加密钥</button>' +
        '</div>' +
        '<div class="card-footer"><button class="btn btn-sm btn-danger" data-rm-model="' + i + '">删除模型</button></div>';

      container.appendChild(card);
    });

    // 供应商下拉 + 模型ID输入 → 合并为 model 字段
    container.querySelectorAll('[data-vf="vendor"]').forEach(function(sel) {
      sel.addEventListener('change', function() {
        var idx = parseInt(sel.dataset.mi);
        var vendor = sel.value;
        var modelIdInput = container.querySelector('input[data-vf="model_id"][data-mi="' + idx + '"]');
        var modelId = modelIdInput ? modelIdInput.value : '';
        if (!rawConfig.picoclaw) rawConfig.picoclaw = {};
        if (!rawConfig.picoclaw.model_list) rawConfig.picoclaw.model_list = [];
        rawConfig.picoclaw.model_list[idx].model = buildModelField(vendor, modelId);
        // 自动填充默认 api_base
        if (VENDORS[vendor] && VENDORS[vendor].api_base) {
          var card = sel.closest('.card');
          var apiBaseInput = card.querySelector('input[data-mf="api_base"]');
          var currentBase = rawConfig.picoclaw.model_list[idx].api_base || '';
          var isOldDefault = Object.keys(VENDORS).some(function(v) {
            return v !== vendor && VENDORS[v].api_base === currentBase;
          });
          if (!currentBase || isOldDefault) {
            rawConfig.picoclaw.model_list[idx].api_base = VENDORS[vendor].api_base;
            apiBaseInput.value = VENDORS[vendor].api_base;
            apiBaseInput.placeholder = VENDORS[vendor].api_base;
          }
        }
      });
    });
    container.querySelectorAll('[data-vf="model_id"]').forEach(function(input) {
      input.addEventListener('change', function() {
        var idx = parseInt(input.dataset.mi);
        var card = input.closest('.card');
        var vendorSel = card.querySelector('select[data-vf="vendor"]');
        var vendor = vendorSel ? vendorSel.value : 'custom';
        if (!rawConfig.picoclaw) rawConfig.picoclaw = {};
        if (!rawConfig.picoclaw.model_list) rawConfig.picoclaw.model_list = [];
        rawConfig.picoclaw.model_list[idx].model = buildModelField(vendor, input.value);
      });
    });

    // 绑定模型字段修改
    container.querySelectorAll('[data-mf]').forEach(function(input) {
      input.addEventListener('change', function() {
        var idx = parseInt(input.dataset.mi);
        var val = input.value;
        if (input.dataset.mf === 'request_timeout' && val !== '') val = Number(val);
        if (!rawConfig.picoclaw) rawConfig.picoclaw = {};
        if (!rawConfig.picoclaw.model_list) rawConfig.picoclaw.model_list = [];
        rawConfig.picoclaw.model_list[idx][input.dataset.mf] = val;
        // 如果改了 model_name，同步更新 security 中的 key 和默认模型下拉
        if (input.dataset.mf === 'model_name') {
          syncSecurityOnRename();
          renderModelList();
        }
      });
    });

    // 删除模型
    container.querySelectorAll('[data-rm-model]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        var idx = parseInt(btn.dataset.rmModel);
        var removedName = rawConfig.picoclaw.model_list[idx].model_name;
        rawConfig.picoclaw.model_list.splice(idx, 1);
        // 同时删除该模型的 API 密钥
        if (removedName && rawConfig.security && rawConfig.security.model_list) {
          var toDelete = [];
          Object.keys(rawConfig.security.model_list).forEach(function(k) {
            var parts = k.split(':');
            if (parts.slice(0, -1).join(':') === removedName) toDelete.push(k);
          });
          toDelete.forEach(function(k) { delete rawConfig.security.model_list[k]; });
        }
        renderModelList();
      });
    });

    // 添加密钥
    container.querySelectorAll('[data-add-key]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        var idx = parseInt(btn.dataset.addKey);
        var modelName = rawConfig.picoclaw.model_list[idx].model_name;
        if (!modelName) { alert('请先填写模型名'); return; }
        var secMap = getSecurityMap();
        if (!secMap[modelName]) secMap[modelName] = [];
        secMap[modelName].push('');
        setSecurityMap(secMap);
        renderModelList();
      });
    });

    // 删除密钥
    container.querySelectorAll('[data-rm-key]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        var parts = btn.dataset.rmKey.split(':').map(Number);
        var modelIdx = parts[0];
        var keyIdx = parts[1];
        var modelName = rawConfig.picoclaw.model_list[modelIdx].model_name;
        var secMap = getSecurityMap();
        if (secMap[modelName]) {
          secMap[modelName].splice(keyIdx, 1);
          if (secMap[modelName].length === 0) delete secMap[modelName];
        }
        setSecurityMap(secMap);
        renderModelList();
      });
    });

    // 修改密钥值
    container.querySelectorAll('[data-sf="keyval"]').forEach(function(input) {
      input.addEventListener('change', function() {
        var modelIdx = parseInt(input.dataset.mi);
        var keyIdx = parseInt(input.dataset.ki);
        var modelName = rawConfig.picoclaw.model_list[modelIdx].model_name;
        var secMap = getSecurityMap();
        if (secMap[modelName]) {
          secMap[modelName][keyIdx] = input.value;
        }
        setSecurityMap(secMap);
      });
    });
  }

  // 当模型名被修改时，同步更新 security 中的 key
  function syncSecurityOnRename() {
    if (!rawConfig.security) rawConfig.security = {};
    if (!rawConfig.security.model_list) return;
    var secList = rawConfig.security.model_list;
    var models = (rawConfig.picoclaw && rawConfig.picoclaw.model_list) || [];
    var modelNames = {};
    models.forEach(function(m) { if (m.model_name) modelNames[m.model_name] = true; });
    // 删除 security 中不再对应任何模型的 key
    Object.keys(secList).forEach(function(k) {
      var parts = k.split(':');
      var name = parts.slice(0, -1).join(':');
      if (!modelNames[name]) delete secList[k];
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

  async function refreshMigrationRules() {
    if (!confirm('确定从远端更新 Picoclaw 迁移规则吗？')) return;
    showMsg('#settings-msg', '正在更新迁移规则...', true);
    try {
      var res = await Api.post('/api/admin/migration-rules/refresh', {});
      showMsg('#settings-msg', res.message || res.error, res.success);
    } catch (e) { showMsg('#settings-msg', e.message, false); }
  }

  // 下发配置到全部用户
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

// pollTaskStatus 轮询任务队列进度
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
