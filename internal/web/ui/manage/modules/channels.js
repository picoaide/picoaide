export async function init(ctx) {
  var $ = ctx.$, esc = ctx.esc, showMsg = ctx.showMsg, Api = ctx.Api;

  var currentChannel = '';
  var currentChannels = [];
  var channelListLoaded = false;

  async function loadChannels() {
    try {
      var data = await Api.get('/api/picoclaw/channels');
      if (!data.success) {
        showMsg('#channel-msg', data.error || '加载失败', false);
        return;
      }
      renderChannelList(data.channels || []);
      channelListLoaded = true;
      if (currentChannel) loadChannelFields();
    } catch (e) {
      showMsg('#channel-msg', e.message, false);
    }
  }

  function renderChannelList(channels) {
    var box = $('#channel-list');
    currentChannels = channels || [];
    if (!channels.length) {
      currentChannel = '';
      box.innerHTML = '';
      $('#channel-fields').innerHTML = '<p class="text-muted">管理员还没有开放可配置的通讯渠道。</p>';
      $('#channel-title').textContent = '渠道配置';
      $('#channel-subtitle').textContent = '管理员开放渠道后可在这里维护配置';
      $('#channel-save-btn').disabled = true;
      return;
    }
    $('#channel-save-btn').disabled = false;
    if (!currentChannel || !channels.some(function(ch) { return ch.key === currentChannel; })) {
      currentChannel = channels[0].key;
    }
    box.innerHTML = channels.map(function(ch) {
      var status = ch.enabled ? '已启用' : (ch.configured ? '已配置' : '未配置');
      var badgeCls = ch.enabled ? 'badge-ok' : (ch.configured ? 'badge-muted' : 'badge-danger');
      return '<button class="nav-item' + (ch.key === currentChannel ? ' active' : '') + '" data-channel-section="' + esc(ch.key) + '">' +
        '<span class="nav-item-main">' +
          '<span class="nav-item-title">' + esc(ch.label || ch.key) + '</span>' +
          '<span class="nav-item-subtitle">' + esc(ch.key) + '</span>' +
        '</span>' +
        '<span class="badge ' + badgeCls + '">' + esc(status) + '</span>' +
      '</button>';
    }).join('');
    box.querySelectorAll('[data-channel-section]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        currentChannel = btn.dataset.channelSection;
        renderChannelList(channels);
        loadChannelFields();
      });
    });
  }

  async function loadChannelFields() {
    if (!channelListLoaded) { return loadChannels(); }
    if (!currentChannel) return;
    try {
      var data = await Api.get('/api/picoclaw/config-fields?section=' + encodeURIComponent(currentChannel));
      if (!data.success) {
        showMsg('#channel-msg', data.error || '加载失败', false);
        return;
      }
      var ch = currentChannels.find(function(item) { return item.key === currentChannel; }) || {};
      $('#channel-title').textContent = ch.label || currentChannel || '渠道配置';
      $('#channel-subtitle').textContent = (ch.enabled ? '当前渠道已启用' : (ch.configured ? '当前渠道已配置但未启用' : '当前渠道尚未配置')) + '，保存后会重启容器';
      renderChannelFields(data.fields || []);
    } catch (e) {
      showMsg('#channel-msg', e.message, false);
    }
  }

  function renderChannelFields(fields) {
    var box = $('#channel-fields');
    if (!fields.length) {
      box.innerHTML = '<p class="text-muted">当前渠道没有可配置字段。</p>';
      return;
    }
    box.innerHTML = fields.map(function(item) {
      var field = item.field || {};
      var value = item.value === undefined || item.value === null ? '' : item.value;
      var fieldType = String(field.type || 'text').toLowerCase();
      var type = fieldType === 'password' ? 'password' : (fieldType === 'boolean' ? 'checkbox' : (fieldType === 'integer' || fieldType === 'number' ? 'number' : 'text'));
      var checked = type === 'checkbox' && value ? ' checked' : '';
      var val = type === 'checkbox' ? '' : ' value="' + esc(formatFieldValue(field, value)) + '"';
      if (fieldType === 'string_list' || fieldType === 'array' || fieldType === 'list' || fieldType === 'json' || fieldType === 'object' || fieldType === 'map') {
        return '<div class="field">' +
          '<label>' + esc(field.label || field.key) + '</label>' +
          '<textarea rows="' + (fieldType === 'json' || fieldType === 'object' || fieldType === 'map' ? '6' : '3') + '" data-channel-field="' + esc(field.key) + '" data-field-type="' + esc(fieldType) + '">' +
          esc(formatFieldValue(field, value)) + '</textarea>' +
        '</div>';
      }
      if (type === 'checkbox') {
        return '<div class="field">' +
          '<label>' + esc(field.label || field.key) + '</label>' +
          '<label class="toggle-switch toggle-switch-field">' +
            '<input type="checkbox" data-channel-field="' + esc(field.key) + '" data-field-type="' + esc(fieldType) + '"' + checked + '>' +
            '<span class="toggle-switch-control" aria-hidden="true"></span>' +
            '<span class="toggle-switch-label">' + esc(value ? '已启用' : '未启用') + '</span>' +
          '</label>' +
        '</div>';
      }
      return '<div class="field">' +
        '<label>' + esc(field.label || field.key) + '</label>' +
        '<input type="' + type + '" data-channel-field="' + esc(field.key) + '" data-field-type="' + esc(fieldType) + '"' + val + checked + '>' +
      '</div>';
    }).join('');
  }

  $('#channel-save-btn').addEventListener('click', async function() {
    var msg = $('#channel-msg');
    showMsg(msg, '保存中...', true);
    try {
      var values = {};
      $('#channel-fields').querySelectorAll('[data-channel-field]').forEach(function(input) {
        values[input.dataset.channelField] = readFieldInputValue(input);
      });
      var res = await Api.post('/api/picoclaw/config-fields', {
        section: currentChannel,
        values: JSON.stringify(values),
      });
      showMsg(msg, res.message || res.error, res.success);
      if (res.success) {
        channelListLoaded = false;
        loadChannels();
      }
    } catch (e) { showMsg(msg, e.message, false); }
  });

  loadChannels();
}

function formatFieldValue(field, value) {
  var fieldType = String((field && field.type) || 'text').toLowerCase();
  if (value === undefined || value === null) return '';
  if (fieldType === 'string_list' || fieldType === 'array' || fieldType === 'list') {
    if (Array.isArray(value)) return value.join('\n');
    return String(value);
  }
  if (fieldType === 'json' || fieldType === 'object' || fieldType === 'map') {
    if (typeof value === 'string') return value;
    try { return JSON.stringify(value, null, 2); } catch { return String(value); }
  }
  return value;
}

function readFieldInputValue(input) {
  var fieldType = String(input.dataset.fieldType || '').toLowerCase();
  if (input.type === 'checkbox') return input.checked;
  if (fieldType === 'integer' || fieldType === 'number') {
    if (input.value.trim() === '') return '';
    return Number(input.value);
  }
  return input.value;
}
