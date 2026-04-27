// PicoAide 配置管理页面

var $ = function(s) { return document.querySelector(s); };
var $$ = function(s) { return document.querySelectorAll(s); };

var currentPath = '';
var currentUser = '';

function showLogin() {
  $('#login-page').classList.remove('hidden');
  $('#manage-page').classList.add('hidden');
  currentUser = '';
}

function showManage(username) {
  $('#login-page').classList.add('hidden');
  $('#manage-page').classList.remove('hidden');
  $('#user-display').textContent = username;
  currentUser = username;
  loadDingTalk();
  checkAuthMode();
  switchTab('dingtalk');
}

// 登录
$('#login-btn').addEventListener('click', async function() {
  var msg = $('#login-msg');
  msg.textContent = '';
  msg.className = 'msg';
  var url = $('#login-server').value.trim().replace(/\/+$/, '');
  if (!url) { showMsg(msg, '请输入服务器地址', false); return; }

  await chrome.storage.local.set({ serverUrl: url });

  try {
    var res = await apiJSON('POST', '/api/login', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formBody({ username: $('#login-user').value.trim(), password: $('#login-pass').value }),
    });
    if (res.success) {
      currentUser = res.username;
      $('#login-pass').value = '';
      showManage(res.username);
    } else {
      showMsg(msg, res.error || '登录失败', false);
    }
  } catch (e) {
    showMsg(msg, e.message, false);
  }
});

$('#logout-btn').addEventListener('click', async function() {
  try { await api('POST', '/api/logout'); } catch {}
  currentUser = '';
  showLogin();
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
  $('#login-server').value = base;
  if (!base) { showLogin(); return; }

  try {
    var data = await apiJSON('GET', '/api/dingtalk');
    if (data.success) { currentUser = currentUser || 'user'; showManage(currentUser); return; }
  } catch {}
  try {
    var data = await apiJSON('GET', '/api/csrf');
    if (data.success) { showManage(currentUser || 'user'); return; }
  } catch {}
  showLogin();
}

// Tab 切换
function switchTab(name) {
  $$('.tab').forEach(function(t) { t.classList.toggle('active', t.dataset.tab === name); });
  $$('.panel').forEach(function(p) { p.classList.toggle('active', p.id === 'tab-' + name); });
  if (name === 'files') loadFiles('');
}

$$('.tab').forEach(function(btn) {
  btn.addEventListener('click', function() { switchTab(btn.dataset.tab); });
});

// 钉钉
async function loadDingTalk() {
  try {
    var data = await apiJSON('GET', '/api/dingtalk');
    if (data.success) {
      $('#dt-client-id').value = data.client_id || '';
      $('#dt-client-secret').value = data.client_secret || '';
    }
  } catch {}
}

$('#dt-save-btn').addEventListener('click', async function() {
  var msg = $('#dt-msg');
  showMsg(msg, '保存中...', true);
  try {
    var csrf = await getCSRF();
    var res = await apiJSON('POST', '/api/dingtalk', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formBody({ client_id: $('#dt-client-id').value.trim(), client_secret: $('#dt-client-secret').value.trim(), csrf_token: csrf }),
    });
    showMsg(msg, res.message || res.error, res.success);
  } catch (e) { showMsg(msg, e.message, false); }
});

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

    var table = document.createElement('table');
    var tbody = document.createElement('tbody');
    for (var j = 0; j < entries.length; j++) {
      (function(entry) {
        var tr = document.createElement('tr');
        tr.style.cursor = 'pointer';
        tr.innerHTML = '<td>' + (entry.is_dir ? '📁 ' : '📄 ') + esc(entry.name) + '</td><td>' + (entry.is_dir ? '' : (entry.size_str || '')) + '</td><td><div class="btn-group">' + (!entry.is_dir ? '<button class="btn btn-sm btn-outline dl-btn">下载</button>' : '') + '<button class="btn btn-sm btn-danger del-btn">删除</button></div></td>';

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
    list.appendChild(table);
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

$('#upload-input').addEventListener('change', async function() {
  var file = $('#upload-input').files[0];
  if (!file) return;
  var msg = $('#file-msg');
  showMsg(msg, '上传中...', true);
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
});

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
