var _q = function(id) { return document.querySelector(id); };

export async function init(ctx) {
  var { Api, showMsg, confirmModal } = ctx;

  _q('#models-save-btn').addEventListener('click', saveConfig);
  _q('#models-test-btn').addEventListener('click', testConnection);
  _q('#models-reset-btn').addEventListener('click', async function() {
    if (await confirmModal('重新加载？未保存的修改将丢失。')) loadConfig();
  });

  await loadConfig();

  async function loadConfig() {
    showMsg('#models-msg', '加载中...', true);
    try {
      var resp = await fetch('/api/config', { method: 'GET', credentials: 'include' });
      var text = await resp.text();
      var cfg = JSON.parse(text);
      if (cfg.success === false) { showMsg('#models-msg', cfg.error, false); return; }
      showMsg('#models-msg', '');
      var m = cfg.model || {};
      setSelect('model-provider', m.provider || 'openai');
      setVal('model-model-id', m.model_id || '');
      setVal('model-base-url', m.base_url || '');
      setVal('model-api-key', m.api_key || '');
      setVal('model-request-timeout', m.request_timeout || '600');
      setVal('model-max-tokens', m.max_tokens || '0');
      setVal('model-context-window', m.context_window || '200000');
      setVal('model-max-iter', m.max_iter || '500');
      setVal('model-temperature', m.temperature || '0.7');
    } catch (e) { showMsg('#models-msg', e.message, false); }
  }

  async function saveConfig() {
    showMsg('#models-msg', '保存中...', true);
    try {
      var resp = await fetch('/api/config', { method: 'GET', credentials: 'include' });
      var cfg = await resp.json();
      if (!cfg.model) cfg.model = {};
      cfg.model.provider = getVal('model-provider');
      cfg.model.model_id = getVal('model-model-id');
      cfg.model.base_url = getVal('model-base-url');
      cfg.model.api_key = getVal('model-api-key');
      cfg.model.request_timeout = numVal('model-request-timeout');
      cfg.model.max_tokens = numVal('model-max-tokens');
      cfg.model.context_window = numVal('model-context-window');
      cfg.model.max_iter = numVal('model-max-iter');
      cfg.model.temperature = numVal('model-temperature');
      var res = await Api.post('/api/config', { config: JSON.stringify(cfg) });
      showMsg('#models-msg', res.message || res.error, res.success);
      } catch (e) { showMsg('#models-msg', e.message, false); }
    }

  async function testConnection() {
    var btn = _q('#models-test-btn');
    btn.disabled = true;
    btn.textContent = '测试中...';
    showMsg('#models-msg', '正在测试连接...', true);
    try {
      var body = new URLSearchParams();
      body.set('provider', getVal('model-provider'));
      body.set('model_id', getVal('model-model-id'));
      body.set('base_url', getVal('model-base-url'));
      body.set('api_key', getVal('model-api-key'));
      var resp = await fetch('/api/admin/model/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: body.toString(),
        credentials: 'include',
      });
      var data = await resp.json();
      showMsg('#models-msg', data.message || data.error || (data.success ? '连接成功' : '连接失败'), data.success);
    } catch (e) {
      showMsg('#models-msg', e.message, false);
    } finally {
      btn.disabled = false;
      btn.textContent = '测试连接';
    }
  }

  function setVal(id, val) {
    var el = _q('#' + id);
    if (el && val !== undefined && val !== null && val !== '') el.value = val;
  }
  function setSelect(id, val) {
    var el = _q('#' + id);
    if (!el || !val) return;
    for (var i = 0; i < el.options.length; i++) {
      if (el.options[i].value === val) { el.selectedIndex = i; return; }
    }
  }
  function getVal(id) {
    var el = _q('#' + id);
    return el ? el.value : '';
  }
  function numVal(id) {
    var el = _q('#' + id);
    if (!el || el.value === '') return 0;
    var n = Number(el.value);
    return isNaN(n) ? 0 : n;
  }
}
