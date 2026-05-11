// PicoAide 统一登录页

var loginForm = document.getElementById('login-form');
var loginUser = document.getElementById('login-user');
var loginPass = document.getElementById('login-pass');
var loginMsg = document.getElementById('login-msg');
var loginBtn = document.getElementById('login-btn');
var loginHeading = document.querySelector('.login-heading p');
var oidcButton;

async function setupLoginMode() {
  try {
    var info = await apiJSON('GET', '/api/login/mode');
    if (info.auth_mode === 'oidc') {
      if (loginHeading) loginHeading.textContent = '普通用户使用企业统一认证，管理员可继续使用本地超管密码。';
      oidcButton = document.createElement('button');
      oidcButton.className = 'btn btn-primary login-submit';
      oidcButton.type = 'button';
      oidcButton.textContent = '企业统一登录';
      oidcButton.addEventListener('click', function() { window.location.href = '/api/login/oidc'; });
      loginForm.parentNode.insertBefore(oidcButton, loginForm);
      loginBtn.textContent = '管理员登录';
    }
  } catch {}
}

function redirectByRole(info) {
  if (info.role === 'superadmin') {
    window.location.replace('/admin/dashboard');
    return;
  }
  if (info.initializing) {
    window.location.replace('/initializing');
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
    if (res.initializing) {
      window.location.replace('/initializing');
      return;
    }
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

document.addEventListener('DOMContentLoaded', function() {
  setupLoginMode();
  checkExistingSession();
});
