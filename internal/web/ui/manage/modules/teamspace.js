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
        return '<div class="card" style="margin-bottom:.5rem">' +
          '<div class="card-header" style="display:flex;align-items:center;gap:.5rem">' +
            '📁 ' + esc(f.name) + ' ' + typeLabel +
          '</div>' +
          '<div class="text-sm text-muted">' + esc(f.description || '') + '</div>' +
          '<div class="text-sm mt-1">成员: ' + f.member_count + ' 人</div>' +
        '</div>';
      }).join('');
    } catch (e) {
      list.innerHTML = '';
      showMsg(msg, e.message, false);
    }
  }
}
