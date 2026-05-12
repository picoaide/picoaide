// PicoAide 统一登录页（provider 无关，根据元信息动态渲染）

var loginForm = document.getElementById('login-form');
var loginUser = document.getElementById('login-user');
var loginPass = document.getElementById('login-pass');
var loginMsg = document.getElementById('login-msg');
var loginBtn = document.getElementById('login-btn');
var loginHeading = document.querySelector('.login-heading p');
var ssoButton;

async function setupLoginMode() {
  try {
    var info = await apiJSON('GET', '/api/login/mode');
    var provider = info.provider;

    // 根据认证源能力动态渲染
    if (provider.has_password && provider.has_browser) {
      // 同时支持密码和 SSO（如 LDAP + 浏览器双模式）
      if (loginHeading) loginHeading.textContent = '管理员使用密码登录，普通用户使用' + provider.display_name + '登录';
    } else if (provider.has_browser) {
      // 仅 SSO（如 OIDC、企业微信）
      if (loginHeading) loginHeading.textContent = '点击下方按钮使用' + provider.display_name + '登录';
      loginForm.style.display = 'none';
    } else {
      // 仅密码（本地模式等）
      if (loginHeading) loginHeading.textContent = '请输入用户名密码登录';
    }

    // 创建 SSO 按钮
    if (provider.has_browser) {
      ssoButton = document.createElement('button');
      ssoButton.className = 'btn btn-primary login-sso';
      ssoButton.type = 'button';
      ssoButton.textContent = provider.display_name + '登录';
      ssoButton.addEventListener('click', function() { window.location.href = '/api/login/auth'; });
      loginForm.parentNode.insertBefore(ssoButton, loginForm.nextSibling);
      if (provider.has_password) {
        loginBtn.textContent = '管理员登录';
      }
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
