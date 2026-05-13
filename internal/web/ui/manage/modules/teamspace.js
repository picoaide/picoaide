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

      list.innerHTML = '<div style="display:flex;flex-wrap:wrap;gap:.5rem">' + folders.map(function(f) {
        var desc = f.description || '';
        if (desc.length > 100) desc = desc.substring(0, 100) + '…';
        var typeLabel = f.is_public ? '<span class="badge badge-ok">公共</span>' : '<span class="badge badge-muted">组共享</span>';
        return '<div class="card" style="cursor:pointer;min-width:220px;flex:1;margin-bottom:0" data-folder="' + esc(f.name) + '">' +
          '<div class="card-header" style="display:flex;align-items:center;gap:.5rem">' +
            '📁 ' + esc(f.name) + ' ' + typeLabel +
          '</div>' +
          (desc ? '<div class="text-sm text-muted" style="padding:0 .75rem">' + esc(desc) + '</div>' : '') +
          '<div class="toolbar" style="padding:.4rem .75rem .6rem">' +
            '<span class="text-sm">' + f.member_count + ' 人</span>' +
            '<button class="btn btn-sm btn-outline">查看成员</button>' +
          '</div>' +
        '</div>';
      }).join('') + '</div>';

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
      : '<div class="group-tags" style="padding:.5rem 0">' +
        members.map(function(m) { return '<span class="tag">' + esc(m.username) + '</span>'; }).join(' ') +
        '</div>';

    ctx.showModal({ title: '成员列表 - ' + folder.name, width: '400px', body: body, footer: [
      { label: '关闭', value: 'close' }
    ]}).catch(function() {});
  }
}
