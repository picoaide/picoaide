export async function init(ctx) {
  var $ = ctx.$, esc = ctx.esc, showMsg = ctx.showMsg, Api = ctx.Api;

  await loadTeamspace();

  async function loadTeamspace() {
    var list = $('#ts-list');
    var msg = $('#ts-msg');
    list.innerHTML = '<progress />';
    msg.textContent = '';
    msg.className = 'msg';

    try {
      var data = await Api.get('/api/shared-folders');
      if (!data.success) { list.innerHTML = ''; showMsg(msg, data.error || '加载失败', false); return; }

      var folders = data.folders || [];
      if (folders.length === 0) {
        list.innerHTML = '<p class="text-muted text-center">你目前没有可访问的共享文件夹</p>';
        return;
      }

      list.innerHTML = folders.map(function(f) {
        var typeLabel = f.is_public ? '<span class="badge badge-ok">公共</span>' : '<span class="badge badge-muted">组共享</span>';
        return '<div class="card" style="margin-bottom:.5rem;cursor:pointer" data-folder="' + esc(f.name) + '">' +
          '<div class="card-header" style="display:flex;align-items:center;gap:.5rem">' +
            '📁 ' + esc(f.name) + ' ' + typeLabel +
          '</div>' +
          '<div class="text-sm text-muted">' + esc(f.description || '') + '</div>' +
          '<div class="toolbar" style="margin-top:.4rem">' +
            '<span class="text-sm">成员: ' + f.member_count + ' 人</span>' +
            '<button class="btn btn-sm btn-outline">查看成员</button>' +
          '</div>' +
        '</div>';
      }).join('');

      list.querySelectorAll('[data-folder]').forEach(function(card) {
        card.addEventListener('click', function() {
          var name = card.dataset.folder;
          var folder = folders.find(function(f) { return f.name === name; });
          if (folder) showMembers(folder);
        });
      });
    } catch (e) {
      list.innerHTML = '';
      showMsg(msg, e.message, false);
    }
  }

  function showMembers(folder) {
    var members = folder.members || [];
    var body = members.length === 0
      ? '<p class="text-muted text-center">暂无成员</p>'
      : '<div class="table-wrap"><table class="compact-table"><thead><tr><th>用户名</th><th>挂载状态</th></tr></thead><tbody>' +
        members.map(function(m) {
          var c = m.mounted ? 'badge-ok' : 'badge-muted';
          var t = m.mounted ? '✓ 已挂载' : '✗ 未挂载';
          return '<tr><td>' + esc(m.username) + '</td><td><span class="badge ' + c + '">' + t + '</span></td></tr>';
        }).join('') +
        '</tbody></table></div>';

    ctx.showModal({ title: '成员列表 - ' + folder.name, width: '480px', body: body, footer: [
      { label: '关闭', value: 'close' }
    ]}).catch(function() {});
  }
}
