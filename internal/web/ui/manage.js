// PicoAide 配置管理页面

var $ = function(s) { return document.querySelector(s); };
var $$ = function(s) { return document.querySelectorAll(s); };

var currentPath = '';
var currentUser = '';
var currentChannel = '';
var currentChannels = [];
var channelListLoaded = false;

function showManage(username) {
  $('#user-display').textContent = username;
  currentUser = username;
  loadChannels();
  checkAuthMode();
  switchTab('channels');
}

async function redirectIfSuperadmin() {
  var info = await apiJSON('GET', '/api/user/info');
  if (info.success && info.role === 'superadmin') {
    window.location.href = '/admin/dashboard';
    return true;
  }
  return false;
}

$('#logout-btn').addEventListener('click', async function() {
  try { await api('POST', '/api/logout'); } catch {}
  currentUser = '';
  window.location.href = '/login';
});

// 认证模式检查
async function checkAuthMode() {
  try {
    var info = await apiJSON('GET', '/api/user/info');
    if (info.success && info.unified_auth) {
      // 统一认证模式：隐藏密码修改 tab
      var tabBtn = $('#tab-password-btn');
      if (tabBtn) tabBtn.classList.add('hidden');
    }
  } catch {}
}

// 修改密码
$('#change-password-btn')?.addEventListener('click', async function() {
  var msg = $('#password-msg');
  var oldPw = $('#old-password').value;
  var newPw = $('#new-password').value;
  var confirmPw = $('#confirm-password').value;

  if (!oldPw || !newPw) { showMsg(msg, '请填写完整', false); return; }
  if (newPw.length < 6) { showMsg(msg, '新密码至少 6 个字符', false); return; }
  if (newPw !== confirmPw) { showMsg(msg, '两次输入的新密码不一致', false); return; }

  try {
    var csrf = await getCSRF();
    var res = await apiJSON('POST', '/api/user/password', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formBody({ old_password: oldPw, new_password: newPw, csrf_token: csrf }),
    });
    showMsg(msg, res.message || res.error, res.success);
    if (res.success) {
      $('#old-password').value = '';
      $('#new-password').value = '';
      $('#confirm-password').value = '';
    }
  } catch (e) { showMsg(msg, e.message, false); }
});

// 自动登录
async function tryAutoLogin() {
  var base = await getServerUrl();
  if (!base) { window.location.href = '/login'; return; }

  try {
    var info = await apiJSON('GET', '/api/user/info');
    if (info.success) {
      if (info.role === 'superadmin') {
        window.location.href = '/admin/dashboard';
        return;
      }
      showManage(info.username || 'user');
      return;
    }
  } catch {}
  window.location.href = '/login';
}

// Tab 切换
function switchTab(name) {
  $$('.tab').forEach(function(t) { t.classList.toggle('active', t.dataset.tab === name); });
  $$('.panel').forEach(function(p) { p.classList.toggle('active', p.id === 'tab-' + name); });
  if (name === 'files') loadFiles('');
  if (name === 'channels') loadChannels();
}

$$('.tab').forEach(function(btn) {
  btn.addEventListener('click', function() { switchTab(btn.dataset.tab); });
});

// 通讯渠道
async function loadChannels() {
  try {
    var data = await apiJSON('GET', '/api/picoclaw/channels');
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
  if (!channelListLoaded) {
    return loadChannels();
  }
  if (!currentChannel) return;
  try {
    var data = await apiJSON('GET', '/api/picoclaw/config-fields?section=' + encodeURIComponent(currentChannel));
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
    var csrf = await getCSRF();
    var values = {};
    $('#channel-fields').querySelectorAll('[data-channel-field]').forEach(function(input) {
      values[input.dataset.channelField] = readFieldInputValue(input);
    });
    var res = await apiJSON('POST', '/api/picoclaw/config-fields', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formBody({ section: currentChannel, values: JSON.stringify(values), csrf_token: csrf }),
    });
    showMsg(msg, res.message || res.error, res.success);
    if (res.success) {
      channelListLoaded = false;
      loadChannels();
    }
  } catch (e) { showMsg(msg, e.message, false); }
});

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

// 文件管理
async function loadFiles(path) {
  currentPath = path;
  var list = $('#file-list');
  var msg = $('#file-msg');
  list.innerHTML = '<progress />';
  msg.textContent = '';
  msg.className = 'msg';

  try {
    var data = await apiJSON('GET', '/api/files?path=' + encodeURIComponent(path));
    if (!data.success) { list.innerHTML = ''; showMsg(msg, data.error || '加载失败', false); return; }

    // 面包屑
    var bc = $('#breadcrumb');
    bc.innerHTML = '';
    var crumbs = data.breadcrumb || [];
    for (var i = 0; i < crumbs.length; i++) {
      if (i > 0) bc.appendChild(document.createTextNode(' / '));
      var a = document.createElement('a');
      a.textContent = crumbs[i].name;
      a.href = '#';
      a.dataset.path = crumbs[i].path;
      a.addEventListener('click', function(e) { e.preventDefault(); loadFiles(this.dataset.path); });
      bc.appendChild(a);
    }

    // 文件列表
    list.innerHTML = '';
    var entries = data.entries || data.files || [];
    if (entries.length === 0) { list.innerHTML = '<p class="text-muted text-center">空目录</p>'; return; }

    var wrap = document.createElement('div');
    wrap.className = 'table-wrap';
    var table = document.createElement('table');
    table.className = 'compact-table';
    var tbody = document.createElement('tbody');
    for (var j = 0; j < entries.length; j++) {
      (function(entry) {
        var tr = document.createElement('tr');
        tr.style.cursor = 'pointer';
        tr.innerHTML = '<td>' + (entry.is_dir ? '📁 ' : '📄 ') + esc(entry.name) + '</td><td>' + (entry.is_dir ? '' : (entry.size_str || '')) + '</td><td class="actions-cell"><div class="btn-group">' + (!entry.is_dir ? '<button class="btn btn-sm btn-outline dl-btn">下载</button>' : '') + '<button class="btn btn-sm btn-danger del-btn">删除</button></div></td>';

        tr.querySelector('td:first-child').addEventListener('click', function() {
          if (entry.is_dir) loadFiles(entry.rel_path);
          else openEditor(entry.rel_path);
        });

        var dlBtn = tr.querySelector('.dl-btn');
        if (dlBtn) dlBtn.addEventListener('click', async function(e) {
          e.stopPropagation();
          var base = await getServerUrl();
          window.open(base + '/api/files/download?path=' + encodeURIComponent(entry.rel_path), '_blank');
        });

        tr.querySelector('.del-btn').addEventListener('click', async function(e) {
          e.stopPropagation();
          if (!confirm('删除 ' + entry.name + '？')) return;
          try {
            var csrf = await getCSRF();
            var res = await apiJSON('POST', '/api/files/delete', {
              headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
              body: formBody({ path: entry.rel_path, csrf_token: csrf }),
            });
            showMsg(msg, res.message || res.error, res.success);
            if (res.success) loadFiles(currentPath);
          } catch (err) { showMsg(msg, err.message, false); }
        });

        tbody.appendChild(tr);
      })(entries[j]);
    }
    table.appendChild(tbody);
    wrap.appendChild(table);
    list.appendChild(wrap);
  } catch (e) {
    list.innerHTML = '';
    showMsg(msg, e.message, false);
  }
}

$('#mkdir-btn').addEventListener('click', async function() {
  var name = prompt('目录名：');
  if (!name) return;
  var msg = $('#file-msg');
  try {
    var csrf = await getCSRF();
    var res = await apiJSON('POST', '/api/files/mkdir', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formBody({ path: currentPath, name: name, csrf_token: csrf }),
    });
    showMsg(msg, res.message || res.error, res.success);
    if (res.success) loadFiles(currentPath);
  } catch (e) { showMsg(msg, e.message, false); }
});

$('#upload-input').addEventListener('change', function() {
  var file = $('#upload-input').files[0];
  $('#upload-file-name').textContent = file ? (file.name + ' (' + formatBytes(file.size) + ')') : '未选择文件';
  $('#upload-btn').disabled = !file;
});

$('#upload-btn').addEventListener('click', async function() {
  var file = $('#upload-input').files[0];
  if (!file) {
    showMsg('#file-msg', '请先选择文件', false);
    return;
  }
  var msg = $('#file-msg');
  showMsg(msg, '上传中...', true);
  $('#upload-btn').disabled = true;
  try {
    var csrf = await getCSRF();
    var formData = new FormData();
    formData.append('file', file);
    formData.append('path', currentPath);
    formData.append('csrf_token', csrf);
    var res = await api('POST', '/api/files/upload', { body: formData });
    var data = await res.json();
    showMsg(msg, data.message || data.error, data.success);
    if (data.success) loadFiles(currentPath);
  } catch (e) { showMsg(msg, e.message, false); }
  $('#upload-input').value = '';
  $('#upload-file-name').textContent = '未选择文件';
  $('#upload-btn').disabled = true;
});

function formatBytes(size) {
  if (!size) return '0 B';
  var units = ['B', 'KB', 'MB', 'GB'];
  var value = size;
  var idx = 0;
  while (value >= 1024 && idx < units.length - 1) {
    value = value / 1024;
    idx++;
  }
  return (idx === 0 ? value : value.toFixed(1)) + ' ' + units[idx];
}

// 编辑器
async function openEditor(path) {
  try {
    var data = await apiJSON('GET', '/api/files/edit?path=' + encodeURIComponent(path));
    if (!data.success) { showMsg('#file-msg', data.error || '无法打开文件', false); return; }
    $$('.panel').forEach(function(p) { p.classList.remove('active'); });
    $('#tab-editor').classList.add('active');
    $('#editor-filename').textContent = data.filename;
    $('#editor-content').value = data.content;
    $('#editor-content').dataset.path = data.path;
    $('#editor-msg').textContent = '';
    $('#editor-msg').className = 'msg';
  } catch (e) { showMsg('#file-msg', e.message, false); }
}

$('#editor-back').addEventListener('click', function() { switchTab('files'); });

$('#editor-save').addEventListener('click', async function() {
  var msg = $('#editor-msg');
  showMsg(msg, '保存中...', true);
  try {
    var csrf = await getCSRF();
    var res = await apiJSON('POST', '/api/files/edit', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formBody({ path: $('#editor-content').dataset.path, content: $('#editor-content').value, csrf_token: csrf }),
    });
    showMsg(msg, res.message || res.error, res.success);
  } catch (e) { showMsg(msg, e.message, false); }
});

document.addEventListener('DOMContentLoaded', tryAutoLogin);
