export async function init(ctx) {
  const { Api, esc, showMsg, $, $$, confirmModal, alertModal, promptModal } = ctx;

  $('#refresh-mcp').addEventListener('click', loadServers);
  $('#create-mcp-btn').addEventListener('click', openCreateModal);

  await loadServers();

  async function loadServers() {
    const tbody = $('#mcp-tbody');
    tbody.innerHTML = '<tr><td colspan="7" class="text-center">加载中...</td></tr>';
    $('#mcp-empty').classList.add('hidden');

    const data = await Api.get('/api/admin/mcp/servers');
    if (!data.success) { tbody.innerHTML = ''; showMsg('#mcp-msg', data.error, false); return; }

    const servers = data.data || [];
    if (servers.length === 0) {
      tbody.innerHTML = '';
      $('#mcp-empty').classList.remove('hidden');
      return;
    }

      tbody.innerHTML = '';
      for (const svr of servers) {
        const tr = document.createElement('tr');
        const id = svr.id;
        const name = String(svr.name || '');
        const transport = String(svr.transport || 'stdio');
        const command = String(svr.command || svr.url || '-');
        const headers = String(svr.headers || '{}');
        const enabled = String(svr.enabled) === '1';
        const statusIcon = enabled ? '<span class="badge badge-ok">启用</span>' : '<span class="badge badge-muted">停用</span>';
        const tc = svr.tool_count;
        const toolCountText = tc >= 0
          ? '<a href="#" class="badge badge-ok tool-count-link" data-tc-name="' + esc(name) + '">' + tc + ' 个工具</a>'
          : '<span class="badge badge-danger">连接失败</span>';

        tr.innerHTML =
          '<td><strong>' + esc(name) + '</strong></td>' +
          '<td>' + esc(transport) + '</td>' +
          '<td><code>' + esc(command) + '</code></td>' +
          '<td>' + (headers !== '{}' ? '<span class="badge badge-ok">有</span>' : '<span class="badge badge-muted">无</span>') + '</td>' +
          '<td>' + statusIcon + '</td>' +
          '<td>' + toolCountText + '</td>' +
          '<td class="actions-cell"><div class="btn-group">' +
            '<button class="btn btn-sm btn-outline" data-edit-id="' + id + '">编辑</button>' +
            '<button class="btn btn-sm btn-outline" data-auth-id="' + id + '" data-auth-name="' + esc(name) + '">授权</button>' +
            '<button class="btn btn-sm btn-danger" data-del-id="' + id + '" data-del-name="' + esc(name) + '">删除</button>' +
          '</div></td>';
        tbody.appendChild(tr);
      }

    // 工具列表点击弹窗
    tbody.querySelectorAll('.tool-count-link').forEach(function(a) {
      a.addEventListener('click', function(e) {
        e.preventDefault();
        openToolListModal(a.dataset.tcName);
      });
    });

    tbody.querySelectorAll('[data-edit-id]').forEach(btn => {
      btn.addEventListener('click', async () => {
        const id = btn.dataset.editId;
        const server = servers.find(s => String(s.id) === id);
        if (server) openEditModal(server);
      });
    });

    tbody.querySelectorAll('[data-auth-id]').forEach(btn => {
      btn.addEventListener('click', () => openAuthModal(btn.dataset.authId, btn.dataset.authName));
    });

    tbody.querySelectorAll('[data-del-id]').forEach(btn => {
      btn.addEventListener('click', async () => {
        const id = btn.dataset.delId;
        const name = btn.dataset.delName;
        if (!await confirmModal('确定删除 MCP 服务「' + name + '」？')) return;
        showMsg('#mcp-msg', '删除中...', true);
        const res = await Api.post('/api/admin/mcp/servers/delete/' + id, {});
        if (res.success) {
          showMsg('#mcp-msg', res.message || '已删除', true);
          await loadServers();
        } else {
          showMsg('#mcp-msg', res.error, false);
        }
      });
    });
  }

  function openCreateModal() {
    $('#mcp-modal')?.remove();
    const overlay = createModal('添加 MCP 服务', buildFormHTML(null), '添加');
    overlay.querySelector('#mcp-submit-btn').addEventListener('click', async () => {
      const data = collectFormData(overlay);
      if (!data.name) { await alertModal('名称不能为空'); return; }
      showMsg('#mcp-msg', '创建中...', true);
      const res = await Api.post('/api/admin/mcp/servers/create', data);
      if (res.success) {
        overlay.remove();
        showMsg('#mcp-msg', res.message || '创建成功', true);
        await loadServers();
      } else {
        showMsg('#mcp-msg', res.error, false);
      }
    });
  }

  function openEditModal(svr) {
    $('#mcp-modal')?.remove();
    const overlay = createModal('编辑 MCP 服务', buildFormHTML(svr), '保存');
    overlay.querySelector('#mcp-submit-btn').addEventListener('click', async () => {
      const data = collectFormData(overlay);
      if (!data.name) { await alertModal('名称不能为空'); return; }
      showMsg('#mcp-msg', '保存中...', true);
      const res = await Api.post('/api/admin/mcp/servers/update/' + svr.id, data);
      if (res.success) {
        overlay.remove();
        showMsg('#mcp-msg', res.message || '保存成功', true);
        await loadServers();
      } else {
        showMsg('#mcp-msg', res.error, false);
      }
    });
  }

  function openAuthModal(serverId, serverName) {
    $('#mcp-auth-modal')?.remove();
    const overlay = document.createElement('div');
    overlay.id = 'mcp-auth-modal';
    overlay.className = 'modal-overlay';
    overlay.innerHTML =
      '<div class="modal" style="width:500px">' +
        '<div class="modal-header">授权管理 — ' + esc(serverName) + '<button id="auth-modal-close">&times;</button></div>' +
        '<div class="modal-body">' +
          '<p class="text-sm text-muted">授权后，指定用户或组的 Agent 可通过 MCP 调用该服务的工具。</p>' +
          '<div id="auth-grants-list" class="mt-1"><small>加载中...</small></div>' +
          '<hr class="my-1">' +
          '<div class="field"><label>授权类型</label>' +
            '<select id="auth-grant-type"><option value="user">用户</option><option value="group">用户组</option><option value="*">所有用户</option></select></div>' +
          '<div class="field"><label>授权对象</label>' +
            '<input type="text" id="auth-grant-value" placeholder="用户名或组名"></div>' +
          '<div id="auth-modal-msg" class="msg"></div>' +
        '</div>' +
        '<div class="modal-footer">' +
          '<button class="btn btn-primary" id="auth-add-btn">添加授权</button>' +
        '</div>' +
      '</div>';
    $('#content-area').appendChild(overlay);
    overlay.querySelector('#auth-modal-close').onclick = () => overlay.remove();
    overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });

    // 选择"所有用户"时，禁用输入框，固定 grant_value='*'
    const grantTypeSel = overlay.querySelector('#auth-grant-type');
    const grantValueInput = overlay.querySelector('#auth-grant-value');
    grantTypeSel.addEventListener('change', function() {
      if (this.value === '*') {
        grantValueInput.disabled = true;
        grantValueInput.placeholder = '所有用户无需输入';
      } else {
        grantValueInput.disabled = false;
        grantValueInput.placeholder = '用户名或组名';
      }
    });

    overlay.querySelector('#auth-add-btn').addEventListener('click', async () => {
      const grantType = grantTypeSel.value;
      const grantValue = grantType === '*' ? '*' : grantValueInput.value.trim();
      if (!grantValue) { await alertModal('请输入授权对象'); return; }
      showMsg('#auth-modal-msg', '添加中...', true);
      const res = await Api.post('/api/admin/mcp/servers/grants/add', {
        server_id: parseInt(serverId),
        grant_type: grantType,
        grant_value: grantValue,
      });
      if (res.success) {
        overlay.querySelector('#auth-grant-value').value = '';
        showMsg('#auth-modal-msg', '授权添加成功', true);
        await loadGrants(overlay, serverId);
      } else {
        showMsg('#auth-modal-msg', res.error, false);
      }
    });

    loadGrants(overlay, serverId);
  }

  async function loadGrants(overlay, serverId) {
    const container = overlay.querySelector('#auth-grants-list');
    container.innerHTML = '<small>加载中...</small>';
    const data = await Api.get('/api/admin/mcp/servers/grants?server_id=' + serverId);
    if (!data.success) { container.innerHTML = '<small>查询失败</small>'; return; }
    const grants = data.data || [];
    if (grants.length === 0) {
      container.innerHTML = '<small class="text-muted">暂无授权，添加授权后即可使用</small>';
      return;
    }
    container.innerHTML = '';
    for (const g of grants) {
      const gt = String(g.grant_type);
      const gv = String(g.grant_value);
      let label;
      if (gt === '*') {
        label = '所有用户';
      } else {
        label = (gt === 'user' ? '用户: ' : '组: ') + gv;
      }
      const id = String(g.id);
      const tag = document.createElement('span');
      tag.className = 'tag';
      tag.innerHTML = esc(label) + ' <button data-remove-grant="' + id + '">&times;</button>';
      tag.querySelector('[data-remove-grant]').addEventListener('click', async () => {
        if (!await confirmModal('移除该授权？')) return;
        const res = await Api.post('/api/admin/mcp/servers/grants/remove/' + id, {});
        if (res.success) { await loadGrants(overlay, serverId); }
      });
      container.appendChild(tag);
    }
  }

  function createModal(title, bodyHTML, submitLabel) {
    const overlay = document.createElement('div');
    overlay.id = 'mcp-modal';
    overlay.className = 'modal-overlay';
    overlay.innerHTML =
      '<div class="modal" style="width:550px">' +
        '<div class="modal-header">' + title + '<button id="modal-close">&times;</button></div>' +
        '<div class="modal-body">' + bodyHTML + '<div id="mcp-modal-msg" class="msg"></div></div>' +
        '<div class="modal-footer"><button class="btn btn-primary" id="mcp-submit-btn">' + submitLabel + '</button></div>' +
      '</div>';
    $('#content-area').appendChild(overlay);
    overlay.querySelector('#modal-close').onclick = () => overlay.remove();
    overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove(); });
    toggleMcpFields(overlay);
    var transportSel = overlay.querySelector('#mcp-transport');
    if (transportSel) transportSel.addEventListener('change', function() { toggleMcpFields(overlay); });
    return overlay;
  }

  function toggleMcpFields(overlay) {
    var isStdio = overlay.querySelector('#mcp-transport').value === 'stdio';
    var cmdField = overlay.querySelector('#mcp-command-field');
    var argsField = overlay.querySelector('#mcp-args-field');
    var urlField = overlay.querySelector('#mcp-url-field');
    var headersField = overlay.querySelector('#mcp-headers-field');
    var envField = overlay.querySelector('#mcp-env-field');
    if (cmdField) cmdField.classList.toggle('hidden', !isStdio);
    if (argsField) argsField.classList.toggle('hidden', !isStdio);
    if (urlField) urlField.classList.toggle('hidden', isStdio);
    if (headersField) headersField.classList.toggle('hidden', isStdio);
    if (envField) envField.classList.toggle('hidden', !isStdio);
  }

  function buildFormHTML(svr) {
    const name = svr ? String(svr.name || '') : '';
    const transport = svr ? String(svr.transport || 'stdio') : 'stdio';
    const command = svr ? String(svr.command || '') : '';
    const args = svr ? String(svr.args || '[]') : '[]';
    const url = svr ? String(svr.url || '') : '';
    const env = svr ? String(svr.env || '{}') : '{}';
    const headers = svr ? String(svr.headers || '{}') : '{}';
    const enabled = svr ? (String(svr.enabled) === '1') : true;
    return '' +
      '<div class="field"><label>名称</label><input type="text" id="mcp-name" value="' + esc(name) + '" placeholder="如 tyc-mcp, git, filesystem"></div>' +
      '<div class="field"><label>传输方式</label>' +
        '<select id="mcp-transport">' +
          '<option value="stdio"' + (transport === 'stdio' ? ' selected' : '') + '>stdio（子进程）</option>' +
          '<option value="http"' + (transport === 'http' ? ' selected' : '') + '>HTTP（远程服务）</option>' +
        '</select></div>' +
      '<div class="field" id="mcp-command-field"><label>启动命令</label>' +
        '<input type="text" id="mcp-command" value="' + esc(command) + '" placeholder="如 npx, uvx, pipx, 或可执行文件路径">' +
        '<small class="text-muted">常用示例：npx, uvx, pipx, node, python, 或 /usr/local/bin/xxx</small></div>' +
      '<div class="field" id="mcp-args-field"><label>参数（JSON 数组）</label>' +
        '<input type="text" id="mcp-args" value="' + esc(args) + '" placeholder="如 [\"-y\", \"@modelcontextprotocol/server-git\"]">' +
        '<small class="text-muted">示例：npx 用 ["-y","包名"]，uvx/pipx 用 ["包名"]</small></div>' +
      '<div class="field hidden" id="mcp-url-field"><label>URL</label><input type="text" id="mcp-url" value="' + esc(url) + '" placeholder="如 https://mcp.example.com/sse"></div>' +
      '<div class="field hidden" id="mcp-headers-field"><label>请求头（JSON 对象）</label>' +
        '<input type="text" id="mcp-headers" value="' + esc(headers) + '" placeholder="如 {&quot;Authorization&quot;:&quot;Bearer sk-xxx&quot;}">' +
        '<small class="text-muted">HTTP 请求的自定义头部，常用于 API Key 认证</small></div>' +
      '<div class="field" id="mcp-env-field"><label>环境变量（JSON 对象）</label>' +
        '<input type="text" id="mcp-env" value="' + esc(env) + '" placeholder="如 {&quot;API_KEY&quot;:&quot;sk-xxx&quot;}">' +
        '<small class="text-muted">传递给 MCP 进程的环境变量，仅 stdio 模式有效</small></div>' +
      '<div class="field"><label>启用</label>' +
        '<select id="mcp-enabled"><option value="true"' + (enabled ? ' selected' : '') + '>启用</option><option value="false"' + (!enabled ? ' selected' : '') + '>停用</option></select></div>';
  }

  async function openToolListModal(name) {
    var overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.innerHTML =
      '<div class="modal" style="width:95%;max-width:1200px;max-height:85vh">' +
        '<div class="modal-header">' + esc(name) + ' — 工具列表<button class="modal-close">&times;</button></div>' +
        '<div class="modal-body" style="overflow-y:auto;overflow-x:hidden;max-height:65vh;padding:8px 12px">' +
          '<div id="tool-list-loading" class="text-center"><small>加载中...</small></div>' +
          '<div id="tool-list-content" class="hidden"></div>' +
        '</div>' +
      '</div>';
    $('#content-area').appendChild(overlay);
    overlay.querySelector('.modal-close').onclick = function() { overlay.remove(); };
    overlay.addEventListener('click', function(e) { if (e.target === overlay) overlay.remove(); });

    var data = await Api.get('/api/admin/mcp/servers/tools?name=' + encodeURIComponent(name));
    $('#tool-list-loading').classList.add('hidden');
    var content = $('#tool-list-content');
    content.classList.remove('hidden');

    if (!data.success || !data.data || data.data.length === 0) {
      content.innerHTML = '<p class="text-muted text-center">暂无工具（连接失败或未授权）</p>';
      return;
    }

    var html = '<table class="compact-table"><thead><tr><th>工具名称</th><th>说明</th></tr></thead><tbody>';
    for (var i = 0; i < data.data.length; i++) {
      var t = data.data[i];
      html += '<tr><td><code>' + esc(t.name) + '</code></td><td>' + esc(t.description || '-') + '</td></tr>';
    }
    html += '</tbody></table>';
    content.innerHTML = html;
  }

  function collectFormData(overlay) {
    return {
      name: overlay.querySelector('#mcp-name').value.trim(),
      transport: overlay.querySelector('#mcp-transport').value,
      command: overlay.querySelector('#mcp-command').value.trim(),
      args: overlay.querySelector('#mcp-args').value.trim() || '[]',
      url: overlay.querySelector('#mcp-url').value.trim(),
      env: overlay.querySelector('#mcp-env').value.trim() || '{}',
      headers: overlay.querySelector('#mcp-headers').value.trim() || '{}',
      enabled: overlay.querySelector('#mcp-enabled').value === 'true',
    };
  }
}
