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
  'nvidia':       { label: 'NVIDIA',         api_base: 'https://integrate.api.nvidia.com/v1' },
  'cerebras':     { label: 'Cerebras',       api_base: 'https://api.cerebras.ai/v1' },
  'mistral':      { label: 'Mistral',        api_base: 'https://api.mistral.ai/v1' },
  'modelscope':   { label: 'ModelScope',     api_base: 'https://api-inference.modelscope.cn/v1' },
  'minimax':      { label: 'MiniMax',        api_base: 'https://api.minimax.chat/v1' },
  'longcat':      { label: 'LongCat',        api_base: '' },
  'vllm':         { label: 'vLLM',           api_base: 'http://localhost:8000/v1' },
  'antigravity':  { label: 'Antigravity',    api_base: '' },
  'claude-cli':   { label: 'Claude CLI',     api_base: '' },
  'codex-cli':    { label: 'Codex CLI',      api_base: '' },
  'github-copilot': { label: 'GitHub Copilot', api_base: '' },
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

  $('#models-save-btn').addEventListener('click', saveConfig);
  $('#models-reset-btn').addEventListener('click', () => { if (confirm('重新加载？未保存的修改将丢失。')) loadConfig(); });
  $('#add-model-btn').addEventListener('click', () => {
    if (!rawConfig.picoclaw) rawConfig.picoclaw = {};
    if (!rawConfig.picoclaw.model_list) rawConfig.picoclaw.model_list = [];
    rawConfig.picoclaw.model_list.push({ model_name: '', provider: '', model: '', api_base: '', request_timeout: 6000, enabled: true });
    renderModelList();
  });

  async function loadConfig() {
    showMsg('#models-msg', '加载中...', true);
    try {
      var base = await getServerUrl();
      var resp = await fetch(base + '/api/config', { method: 'GET', credentials: 'include' });
      var text = await resp.text();
      try { var e = JSON.parse(text); if (e.success === false) { showMsg('#models-msg', e.error, false); return; } } catch {}
      rawConfig = JSON.parse(text);
      showMsg('#models-msg', '');
      renderConfig();
    } catch (e) { showMsg('#models-msg', e.message, false); }
  }

  function renderConfig() {
    $$('input[data-path]').forEach(function(input) {
      var val = deepGet(rawConfig, input.dataset.path);
      if (val !== undefined && val !== null) input.value = val;
    });
    renderModelList();
  }

  function getSecurityMap() {
    var secList = (rawConfig.security && rawConfig.security.model_list) || {};
    var map = {};
    Object.keys(secList).forEach(function(key) {
      var parts = key.split(':');
      var modelName = parts.slice(0, -1).join(':');
      var keys = Array.isArray(secList[key].api_keys) ? secList[key].api_keys : [];
      if (!map[modelName]) map[modelName] = [];
      map[modelName] = map[modelName].concat(keys);
    });
    return map;
  }

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
    models.forEach(function(m) {
      if (m && m.enabled === undefined) m.enabled = true;
    });
    var secMap = getSecurityMap();

    var defaultSelect = $('#default-model-select');
    if (defaultSelect) {
      var currentDefault = deepGet(rawConfig, 'picoclaw.agents.defaults.model_name') ||
        deepGet(rawConfig, 'picoclaw.agents.defaults.model') || '';
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
        '<div class="toolbar">' +
          '<div><div class="toolbar-title">' + ctx.esc(m.model_name || '未命名模型') + '</div><div class="toolbar-subtitle">' + ctx.esc(m.model || '未填写模型 ID') + '</div></div>' +
          '<label class="toggle-switch toggle-switch-field"><input type="checkbox" ' + (m.enabled !== false ? 'checked' : '') + ' data-mi="' + i + '" data-mf="enabled"><span class="toggle-switch-control" aria-hidden="true"></span><span class="toggle-switch-label">启用</span></label>' +
        '</div>' +
        '<div class="form-section">' +
          '<div class="form-section-title">基础配置</div>' +
          '<div class="grid-2">' +
            '<div class="field"><label>模型名</label><input type="text" value="' + ctx.esc(m.model_name || '') + '" data-mi="' + i + '" data-mf="model_name"></div>' +
            '<div class="field"><label>Provider</label><input type="text" value="' + ctx.esc(m.provider || '') + '" data-mi="' + i + '" data-mf="provider" placeholder="可留空，自动从模型前缀推断"></div>' +
          '</div>' +
          '<div class="grid-2">' +
            '<div class="field"><label>供应商 / 模型</label><div class="row">' +
              '<select data-mi="' + i + '" data-vf="vendor" style="min-width:120px">' + vendorOptions + '</select>' +
              '<input type="text" value="' + ctx.esc(parsed.modelId) + '" data-mi="' + i + '" data-vf="model_id" placeholder="模型 ID" style="flex:1">' +
            '</div></div>' +
            '<div class="field"><label>API Base</label><input type="text" value="' + ctx.esc(apiBaseValue) + '" data-mi="' + i + '" data-mf="api_base" placeholder="' + ctx.esc(defaultBase) + '"></div>' +
          '</div>' +
          '<div class="grid-2">' +
            '<div class="field"><label>超时(秒)</label><input type="number" value="' + (m.request_timeout || '') + '" data-mi="' + i + '" data-mf="request_timeout"></div>' +
            '<div class="field"><label>RPM</label><input type="number" value="' + (m.rpm || '') + '" data-mi="' + i + '" data-mf="rpm"></div>' +
          '</div>' +
          '<div class="field"><label>API 密钥</label>' + keysHtml +
            '<button class="btn btn-sm btn-outline mt-1" data-add-key="' + i + '">+ 添加密钥</button>' +
          '</div>' +
        '</div>' +
        '<div class="form-section">' +
          '<button class="btn btn-sm btn-outline" data-toggle-advanced="' + i + '" type="button">高级配置</button>' +
          '<div class="advanced-fields mt-1" data-advanced="' + i + '">' +
            '<div class="grid-2">' +
              '<div class="field"><label>代理</label><input type="text" value="' + ctx.esc(m.proxy || '') + '" data-mi="' + i + '" data-mf="proxy"></div>' +
              '<div class="field"><label>认证方式</label><input type="text" value="' + ctx.esc(m.auth_method || '') + '" data-mi="' + i + '" data-mf="auth_method" placeholder="oauth / token"></div>' +
            '</div>' +
            '<div class="grid-2">' +
              '<div class="field"><label>连接模式</label><input type="text" value="' + ctx.esc(m.connect_mode || '') + '" data-mi="' + i + '" data-mf="connect_mode" placeholder="stdio / grpc"></div>' +
              '<div class="field"><label>Workspace</label><input type="text" value="' + ctx.esc(m.workspace || '') + '" data-mi="' + i + '" data-mf="workspace"></div>' +
            '</div>' +
            '<div class="grid-2">' +
              '<div class="field"><label>Thinking Level</label><input type="text" value="' + ctx.esc(m.thinking_level || '') + '" data-mi="' + i + '" data-mf="thinking_level" placeholder="off / low / medium / high / xhigh / adaptive"></div>' +
              '<div class="field"><label>Max Tokens 字段</label><input type="text" value="' + ctx.esc(m.max_tokens_field || '') + '" data-mi="' + i + '" data-mf="max_tokens_field"></div>' +
            '</div>' +
            '<div class="field"><label>User Agent</label><input type="text" value="' + ctx.esc(m.user_agent || '') + '" data-mi="' + i + '" data-mf="user_agent"></div>' +
            '<div class="field"><label>Fallbacks</label><textarea rows="2" data-mi="' + i + '" data-mf="fallbacks" data-mf-type="string_list">' + ctx.esc(Array.isArray(m.fallbacks) ? m.fallbacks.join('\\n') : '') + '</textarea></div>' +
            '<div class="grid-2">' +
              '<div class="field"><label>Extra Body JSON</label><textarea rows="4" data-mi="' + i + '" data-mf="extra_body" data-mf-type="json">' + ctx.esc(formatJSON(m.extra_body)) + '</textarea></div>' +
              '<div class="field"><label>Custom Headers JSON</label><textarea rows="4" data-mi="' + i + '" data-mf="custom_headers" data-mf-type="json">' + ctx.esc(formatJSON(m.custom_headers)) + '</textarea></div>' +
            '</div>' +
          '</div>' +
        '</div>' +
        '<div class="card-footer"><button class="btn btn-sm btn-danger" data-rm-model="' + i + '">删除模型</button></div>';

      container.appendChild(card);
    });

    container.querySelectorAll('[data-vf="vendor"]').forEach(function(sel) {
      sel.addEventListener('change', function() {
        var idx = parseInt(sel.dataset.mi);
        var vendor = sel.value;
        var modelIdInput = container.querySelector('input[data-vf="model_id"][data-mi="' + idx + '"]');
        var modelId = modelIdInput ? modelIdInput.value : '';
        ensureModelList();
        rawConfig.picoclaw.model_list[idx].model = buildModelField(vendor, modelId);
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
        ensureModelList();
        rawConfig.picoclaw.model_list[idx].model = buildModelField(vendor, input.value);
      });
    });

    container.querySelectorAll('[data-mf]').forEach(function(input) {
      input.addEventListener('change', function() {
        var idx = parseInt(input.dataset.mi);
        var val = modelInputValue(input);
        ensureModelList();
        if (val === '' || val === null || (Array.isArray(val) && val.length === 0)) delete rawConfig.picoclaw.model_list[idx][input.dataset.mf];
        else rawConfig.picoclaw.model_list[idx][input.dataset.mf] = val;
        if (input.dataset.mf === 'model_name') {
          syncSecurityOnRename();
          renderModelList();
        }
      });
    });

    container.querySelectorAll('[data-rm-model]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        var idx = parseInt(btn.dataset.rmModel);
        var removedName = rawConfig.picoclaw.model_list[idx].model_name;
        rawConfig.picoclaw.model_list.splice(idx, 1);
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

    container.querySelectorAll('[data-toggle-advanced]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        var panel = container.querySelector('[data-advanced="' + btn.dataset.toggleAdvanced + '"]');
        if (panel) panel.classList.toggle('open');
      });
    });
  }

  function syncSecurityOnRename() {
    if (!rawConfig.security) rawConfig.security = {};
    if (!rawConfig.security.model_list) return;
    var secList = rawConfig.security.model_list;
    var models = (rawConfig.picoclaw && rawConfig.picoclaw.model_list) || [];
    var modelNames = {};
    models.forEach(function(m) { if (m.model_name) modelNames[m.model_name] = true; });
    Object.keys(secList).forEach(function(k) {
      var parts = k.split(':');
      var name = parts.slice(0, -1).join(':');
      if (!modelNames[name]) delete secList[k];
    });
  }

  function ensureModelList() {
    if (!rawConfig.picoclaw) rawConfig.picoclaw = {};
    if (!rawConfig.picoclaw.model_list) rawConfig.picoclaw.model_list = [];
  }

  async function saveConfig() {
    showMsg('#models-msg', '保存中...', true);
    collectFields();
    try {
      var res = await Api.post('/api/config', { config: JSON.stringify(rawConfig) });
      showMsg('#models-msg', res.message || res.error, res.success);
    } catch (e) { showMsg('#models-msg', e.message, false); }
  }

  function collectFields() {
    $$('input[data-path]').forEach(function(input) {
      deepSet(rawConfig, input.dataset.path, input.type === 'number' && input.value !== '' ? Number(input.value) : input.value);
    });
    $$('select[data-path]').forEach(function(sel) {
      deepSet(rawConfig, sel.dataset.path, sel.value);
      if (sel.dataset.path === 'picoclaw.agents.defaults.model_name') {
        deepSet(rawConfig, 'picoclaw.agents.defaults.model', sel.value);
      }
    });
  }
}

function formatJSON(value) {
  if (value === undefined || value === null) return '';
  try { return JSON.stringify(value, null, 2); } catch { return String(value); }
}

function modelInputValue(input) {
  var key = input.dataset.mf;
  if (input.type === 'checkbox') return input.checked;
  if ((key === 'request_timeout' || key === 'rpm') && input.value !== '') return Number(input.value);
  if (input.dataset.mfType === 'string_list') {
    return input.value.split(/\n|,/).map(function(v) { return v.trim(); }).filter(Boolean);
  }
  if (input.dataset.mfType === 'json') {
    var text = input.value.trim();
    if (!text) return null;
    return JSON.parse(text);
  }
  return input.value;
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
