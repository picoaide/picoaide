var rawConfig = {};
var channels = [];

export async function init(ctx) {
  const { Api, esc, showMsg, $ } = ctx;

  $('#picoclaw-load').addEventListener('click', loadConfig);
  $('#picoclaw-save').addEventListener('click', savePolicy);

  await loadConfig();

  async function loadConfig() {
    showMsg('#picoclaw-msg', '加载中...', true);
    try {
      rawConfig = await Api.get('/api/config');
      var channelResp = await Api.get('/api/admin/picoclaw/channels');
      channels = channelResp.channels || [];
      renderPolicy();
      showMsg('#picoclaw-msg', '');
    } catch (e) {
      showMsg('#picoclaw-msg', e.message, false);
    }
  }

  function renderPolicy() {
    var box = $('#picoclaw-channel-policy');
    if (!channels.length) {
      box.innerHTML = '<p class="text-muted">当前配置适配包没有可用渠道定义。</p>';
      return;
    }
    box.className = 'channel-policy-grid';
    box.innerHTML = channels.map(function(ch) {
      var enabled = !!(
        deepGet(rawConfig, 'picoclaw.channel_list.' + ch.key + '.enabled') ||
        deepGet(rawConfig, 'picoclaw.channels.' + ch.key + '.enabled')
      );
      return '<label class="channel-policy-card' + (enabled ? ' is-enabled' : '') + '">' +
        '<input type="checkbox" data-channel="' + esc(ch.key) + '"' + (enabled ? ' checked' : '') + '>' +
        '<span class="channel-policy-main">' +
          '<span class="channel-policy-name">' + esc(ch.label || ch.key) + '</span>' +
          '<span class="channel-policy-key">' + esc(ch.key) + '</span>' +
        '</span>' +
        '<span class="toggle-switch-control" aria-hidden="true"></span>' +
      '</label>';
    }).join('');
    box.querySelectorAll('[data-channel]').forEach(function(input) {
      input.addEventListener('change', function() {
        var card = input.closest('.channel-policy-card');
        if (card) card.classList.toggle('is-enabled', input.checked);
      });
    });
  }

  async function savePolicy() {
    if (!rawConfig.picoclaw) rawConfig.picoclaw = {};
    if (!rawConfig.picoclaw.channel_list) rawConfig.picoclaw.channel_list = {};
    if (!rawConfig.picoclaw.channels) rawConfig.picoclaw.channels = {};
    var known = {};
    channels.forEach(function(ch) { known[ch.key] = true; });
    $('#picoclaw-channel-policy').querySelectorAll('[data-channel]').forEach(function(input) {
      var key = input.dataset.channel;
      if (!rawConfig.picoclaw.channel_list[key]) rawConfig.picoclaw.channel_list[key] = {};
      if (!rawConfig.picoclaw.channels[key]) rawConfig.picoclaw.channels[key] = {};
      rawConfig.picoclaw.channel_list[key].enabled = input.checked;
      rawConfig.picoclaw.channel_list[key].type = key;
      rawConfig.picoclaw.channels[key].enabled = input.checked;
    });
    Object.keys(rawConfig.picoclaw.channel_list).forEach(function(key) {
      if (!known[key]) delete rawConfig.picoclaw.channel_list[key];
    });
    Object.keys(rawConfig.picoclaw.channels).forEach(function(key) {
      if (!known[key]) delete rawConfig.picoclaw.channels[key];
    });
    showMsg('#picoclaw-msg', '保存中...', true);
    try {
      var res = await Api.post('/api/config', { config: JSON.stringify(rawConfig) });
      showMsg('#picoclaw-msg', res.message || res.error, res.success);
    } catch (e) {
      showMsg('#picoclaw-msg', e.message, false);
    }
  }
}

function deepGet(obj, path) {
  var keys = path.split('.');
  var cur = obj;
  for (var i = 0; i < keys.length; i++) {
    if (cur == null) return undefined;
    cur = cur[keys[i]];
  }
  return cur;
}
