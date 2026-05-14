export async function init(ctx) {
  var $ = ctx.$, showMsg = ctx.showMsg, Api = ctx.Api;

  // 统一认证模式检查
  (async function() {
    try {
      var info = await Api.get('/api/user/info');
      if (info.success && info.unified_auth) {
        var tip = $('#password-unified-tip');
        if (tip) tip.classList.remove('hidden');
      }
    } catch {}
  })();

  $('#change-password-btn').addEventListener('click', async function() {
    var msg = $('#password-msg');
    var oldPw = $('#old-password').value;
    var newPw = $('#new-password').value;
    var confirmPw = $('#confirm-password').value;

    if (!oldPw || !newPw) { showMsg(msg, '请填写完整', false); return; }
    if (newPw.length < 6) { showMsg(msg, '新密码至少 6 个字符', false); return; }
    if (newPw !== confirmPw) { showMsg(msg, '两次输入的新密码不一致', false); return; }

    try {
      var res = await Api.post('/api/user/password', {
        old_password: oldPw,
        new_password: newPw,
      });
      showMsg(msg, res.message || res.error, res.success);
      if (res.success) {
        $('#old-password').value = '';
        $('#new-password').value = '';
        $('#confirm-password').value = '';
      }
    } catch (e) { showMsg(msg, e.message, false); }
  });
}
