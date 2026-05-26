var rawConfig = {};
var channels = [];

export async function init(ctx) {
  const { Api, esc, showMsg, $ } = ctx;

  $('#channels-load').addEventListener('click', loadConfig);
  $('#channels-save').addEventListener('click', savePolicy);

  await loadConfig();

  async function loadConfig() {
    showMsg('#channels-msg', '加载中...', true);
    try {
      rawConfig = await Api.get('/api/config');
      var channelResp = await Api.get('/api/admin/channels');
      channels = channelResp.channels || [];
      renderPolicy();
      showMsg('#channels-msg', '');
    } catch (e) {
      showMsg('#channels-msg', e.message, false);
    }
  }

  function renderPolicy() {
    var box = $('#channels-channel-policy');
    if (!channels.length) {
      box.innerHTML = '<p class="text-muted">暂无可用的通讯渠道。</p>';
      return;
    }
    box.className = 'channel-policy-grid';
    box.innerHTML = channels.map(function(ch) {
      var enabled = !!deepGet(rawConfig, 'channel.' + ch.key + '.enabled');
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
    var known = {};
    channels.forEach(function(ch) { known[ch.key] = true; });
    // 清理旧 key
    if (!rawConfig.channel) rawConfig.channel = {};
    $('#channels-channel-policy').querySelectorAll('[data-channel]').forEach(function(input) {
      var key = input.dataset.channel;
      if (!rawConfig.channel[key]) rawConfig.channel[key] = {};
      rawConfig.channel[key].enabled = input.checked;
    });
    // 清理不再存在的渠道
    Object.keys(rawConfig.channel).forEach(function(key) {
      if (!known[key]) delete rawConfig.channel[key];
    });
    showMsg('#channels-msg', '保存中...', true);
    try {
      var res = await Api.post('/api/config', { config: JSON.stringify(rawConfig) });
      showMsg('#channels-msg', res.message || res.error, res.success);
    } catch (e) {
      showMsg('#channels-msg', e.message, false);
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
