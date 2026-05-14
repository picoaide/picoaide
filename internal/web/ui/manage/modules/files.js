var tagTooltips = {
  '定时': '定时任务目录。AI 会自动执行此目录下的可执行文件（脚本或二进制），用于定时触发任务。',
  '记忆': 'AI 长期记忆存储目录。AI 在此读写记忆文件，实现跨会话信息保持。',
  '会话': '会话历史记录目录。记录每一次对话，用于上下文恢复和断点续聊。',
  '状态': 'AI 运行状态目录。包含当前环境状态信息，用于故障恢复和会话连续性。',
  '技能': 'AI 技能目录。存放技能包文件，用于扩展 AI 的能力边界。',
  '行为': '定义 AI 代理的行为模式和角色设定。AI 启动时读取此文件确定行为规范。',
  '心跳': 'AI 心跳检测文件。代理定期更新此文件，用于监控进程是否正常运行。',
  '身份': 'AI 身份标识配置文件。定义 AI 的名称、版本和身份信息。',
  '灵魂': 'AI 核心人格和价值观设定。定义 AI 的性格特质和回答风格。',
  '偏好': '用户偏好设置。AI 读取此文件了解用户的使用习惯和偏好。',
  '共享': '团队共享文件夹。此目录挂载了团队协作空间，所有成员共享文件。',
};

export async function init(ctx) {
  var $ = ctx.$, esc = ctx.esc, showMsg = ctx.showMsg, Api = ctx.Api;
  var currentPath = '';

  await loadFiles('');

  $('#mkdir-btn').addEventListener('click', async function() {
    var name = await ctx.promptModal('输入新目录名：');
    if (!name) return;
    var msg = $('#file-msg');
    try {
      var res = await Api.post('/api/files/mkdir', { path: currentPath, name: name });
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
    if (!file) { showMsg('#file-msg', '请先选择文件', false); return; }
    var msg = $('#file-msg');
    showMsg(msg, '上传中...', true);
    $('#upload-btn').disabled = true;
    try {
      var csrf = await ctx.getCSRF();
      var formData = new FormData();
      formData.append('file', file);
      formData.append('path', currentPath);
      formData.append('csrf_token', csrf);
      var res = await fetch('/api/files/upload', {
        method: 'POST', credentials: 'include', body: formData,
      });
      var data = await res.json();
      showMsg(msg, data.message || data.error, data.success);
      if (data.success) loadFiles(currentPath);
    } catch (e) { showMsg(msg, e.message, false); }
    $('#upload-input').value = '';
    $('#upload-file-name').textContent = '未选择文件';
    $('#upload-btn').disabled = true;
  });

  $('#editor-back').addEventListener('click', function() {
    $('#tab-editor').classList.add('hidden');
    $('#file-list').classList.remove('hidden');
  });

  $('#editor-save').addEventListener('click', async function() {
    var msg = $('#editor-msg');
    showMsg(msg, '保存中...', true);
    try {
      var res = await Api.post('/api/files/edit', {
        path: $('#editor-content').dataset.path,
        content: $('#editor-content').value,
      });
      showMsg(msg, res.message || res.error, res.success);
    } catch (e) { showMsg(msg, e.message, false); }
  });

  async function loadFiles(path) {
    currentPath = path;
    var list = $('#file-list');
    var msg = $('#file-msg');
    list.innerHTML = '<progress />';
    msg.textContent = '';
    msg.className = 'msg';

    try {
      var data = await Api.get('/api/files?path=' + encodeURIComponent(path));
      if (!data.success) { list.innerHTML = ''; showMsg(msg, data.error || '加载失败', false); return; }

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

      list.innerHTML = '';
      list.classList.remove('hidden');
      $('#tab-editor').classList.add('hidden');
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
          tr.innerHTML = '<td>' + (entry.is_dir ? '📁 ' : '📄 ') + esc(entry.name) + '</td><td>' + (entry.tag ? '<span class="file-tag" title="' + esc(tagTooltips[entry.tag] || '') + '">' + esc(entry.tag) + '</span>' : '') + '</td><td>' + (entry.is_dir ? '' : (entry.size_str || '')) + '</td><td class="actions-cell"><div class="btn-group">' + (!entry.is_dir ? '<button class="btn btn-sm btn-outline dl-btn">下载</button>' : '') + '<button class="btn btn-sm btn-danger del-btn">删除</button></div></td>';

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
            if (!await ctx.confirmModal('删除 ' + esc(entry.name) + '？')) return;
            try {
              var res = await Api.post('/api/files/delete', { path: entry.rel_path });
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

  async function openEditor(path) {
    try {
      var data = await Api.get('/api/files/edit?path=' + encodeURIComponent(path));
      if (!data.success) { showMsg('#file-msg', data.error || '无法打开文件', false); return; }
      $('#file-list').classList.add('hidden');
      $('#tab-editor').classList.remove('hidden');
      $('#editor-filename').textContent = data.filename;
      $('#editor-content').value = data.content;
      $('#editor-content').dataset.path = data.path;
      $('#editor-msg').textContent = '';
      $('#editor-msg').className = 'msg';
    } catch (e) { showMsg('#file-msg', e.message, false); }
  }
}

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
