// PicoAide 统一登录页

var loginForm = document.getElementById('login-form');
var loginUser = document.getElementById('login-user');
var loginPass = document.getElementById('login-pass');
var loginMsg = document.getElementById('login-msg');
var loginBtn = document.getElementById('login-btn');

function redirectByRole(info) {
  if (info.role === 'superadmin') {
    window.location.replace('/admin/');
    return;
  }
  window.location.replace('/manage');
}

async function checkExistingSession() {
  try {
    var info = await apiJSON('GET', '/api/user/info');
    if (info.success) redirectByRole(info);
  } catch {}
}

loginForm.addEventListener('submit', async function(e) {
  e.preventDefault();
  showMsg(loginMsg, '', false);
  loginBtn.disabled = true;
  loginBtn.textContent = '登录中...';

  try {
    var res = await apiJSON('POST', '/api/login', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formBody({ username: loginUser.value.trim(), password: loginPass.value }),
    });
    if (!res.success) {
      showMsg(loginMsg, res.error || '登录失败', false);
      return;
    }
    loginPass.value = '';
    var info = await apiJSON('GET', '/api/user/info');
    if (!info.success) {
      showMsg(loginMsg, '无法获取用户信息', false);
      return;
    }
    redirectByRole(info);
  } catch (err) {
    showMsg(loginMsg, err.message, false);
  } finally {
    loginBtn.disabled = false;
    loginBtn.textContent = '登录';
  }
});

document.addEventListener('DOMContentLoaded', checkExistingSession);
