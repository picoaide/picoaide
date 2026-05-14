function formatTime(utcStr) {
  if (!utcStr) return '';
  var d = new Date(utcStr);
  if (isNaN(d.getTime())) return utcStr;
  return d.toLocaleString(undefined, {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit'
  });
}

export async function init(ctx) {
  var { Api, esc, showMsg, $, confirmModal } = ctx;
  var container = $('#cookie-list');
  var empty = $('#empty-state');

  async function load() {
    var data = await Api.get('/api/user/cookies');
    if (!data.success) {
      showMsg('#cookies-msg', data.error || '加载失败', false);
      return;
    }
    var list = data.list || [];
    if (list.length === 0) {
      container.innerHTML = '';
      empty.classList.remove('hidden');
      return;
    }
    empty.classList.add('hidden');

    var html = '<div style="display:grid;grid-template-columns:1fr auto auto;gap:.5rem;padding:.75rem 1rem;border-bottom:1px solid var(--border-light);font-size:.82rem;color:var(--muted);font-weight:600;text-transform:uppercase;letter-spacing:.03em">' +
      '<div>域名</div>' +
      '<div style="min-width:160px">授权时间</div>' +
      '<div style="width:80px;text-align:center">操作</div>' +
    '</div>';

    for (var i = 0; i < list.length; i++) {
      var item = list[i];
      html += '<div style="display:grid;grid-template-columns:1fr auto auto;gap:.5rem;padding:.7rem 1rem;border-bottom:1px solid var(--border-light);align-items:center;transition:background .15s" onmouseenter="this.style.background=\'var(--bg-alt)\'" onmouseleave="this.style.background=\'\'">' +
        '<div style="font-weight:600;font-size:.93rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + esc(item.domain) + '</div>' +
        '<div style="color:var(--muted);font-size:.85rem;min-width:160px">' + esc(formatTime(item.updated_at)) + '</div>' +
        '<div style="text-align:center"><button class="btn btn-sm btn-danger delete-btn" data-domain="' + esc(item.domain) + '">取消授权</button></div>' +
      '</div>';
    }
    container.innerHTML = html;

    container.querySelectorAll('.delete-btn').forEach(function(btn) {
      btn.addEventListener('click', async function() {
        var domain = btn.dataset.domain;
        if (!await confirmModal('确定取消 ' + domain + ' 的授权？AI 技能将无法再使用此站点的 Cookie。')) return;
        btn.disabled = true;
        btn.textContent = '取消中...';
        var res = await Api.post('/api/user/cookies/delete', { domain: domain });
        showMsg('#cookies-msg', res.message || res.error, res.success);
        if (res.success) load();
        else { btn.disabled = false; btn.textContent = '取消授权'; }
      });
    });
  }

  await load();
}
