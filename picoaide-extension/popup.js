// PicoAide Helper — popup

async function getServerUrl() {
  const { serverUrl } = await chrome.storage.local.get('serverUrl');
  return (serverUrl || '').replace(/\/+$/, '');
}

async function api(method, path, opts = {}) {
  const base = await getServerUrl();
  if (!base) throw new Error('未设置服务器地址');
  return fetch(`${base}${path}`, { method, credentials: 'include', ...opts });
}

async function apiJSON(method, path, opts = {}) {
  const res = await api(method, path, opts);
  return res.json();
}

function formBody(obj) {
  return Object.entries(obj)
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
    .join('&');
}

function setStatus(text, type = '') {
  const el = document.getElementById('status');
  el.textContent = text;
  el.className = 'status' + (type ? ' ' + type : '');
}

// DOM
const loginView = document.getElementById('login-view');
const mainView = document.getElementById('main-view');
const serverUrlInput = document.getElementById('server-url');
const usernameInput = document.getElementById('username');
const passwordInput = document.getElementById('password');
const loginError = document.getElementById('login-error');
const loginBtn = document.getElementById('login-btn');
const userDisplay = document.getElementById('user-display');
const logoutBtn = document.getElementById('logout-btn');
const syncBtn = document.getElementById('sync-btn');
const cdpBtn = document.getElementById('cdp-btn');
const manageBtn = document.getElementById('manage-btn');
const adminBtn = document.getElementById('admin-btn');

function showLogin() {
  loginView.style.display = '';
  mainView.style.display = 'none';
}

function showMain(username) {
  loginView.style.display = 'none';
  mainView.style.display = '';
  userDisplay.textContent = username;
  setStatus('已连接', 'ok');
  checkAdminRole();
  updateCdpUI();
}

async function checkAdminRole() {
  try {
    const info = await apiJSON('GET', '/api/user/info');
    if (info.success && info.role === 'superadmin') {
      adminBtn.style.display = '';
    } else {
      adminBtn.style.display = 'none';
    }
  } catch {
    adminBtn.style.display = 'none';
  }
}

// --- 登录 ---
loginBtn.addEventListener('click', async () => {
  loginError.textContent = '';
  const url = serverUrlInput.value.trim().replace(/\/+$/, '');
  if (!url) { loginError.textContent = '请输入服务器地址'; return; }

  await chrome.storage.local.set({ serverUrl: url });

  try {
    const res = await apiJSON('POST', '/api/login', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formBody({
        username: usernameInput.value.trim(),
        password: passwordInput.value,
      }),
    });
    if (res.success) {
      passwordInput.value = '';
      showMain(res.username);
    } else {
      loginError.textContent = res.error || '登录失败';
    }
  } catch (e) {
    loginError.textContent = e.message;
  }
});

// --- 退出 ---
logoutBtn.addEventListener('click', async () => {
  try { await api('POST', '/api/logout'); } catch {}
  showLogin();
});

// --- 同步登录状态 ---
syncBtn.addEventListener('click', async () => {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.url) { setStatus('无法获取当前页面', 'err'); return; }

  const domain = new URL(tab.url).hostname;
  setStatus('正在同步 ' + domain + ' 的登录状态...', '');

  try {
    const cookies = await chrome.cookies.getAll({ domain });
    if (cookies.length === 0) { setStatus('当前网站没有可同步的登录状态', 'err'); return; }

    const body = new FormData();
    body.append('domain', domain);
    body.append('cookies', JSON.stringify(cookies));

    const res = await api('POST', '/api/cookies', { body });
    const data = await res.json();

    if (data.success) {
      setStatus('已同步 ' + domain + ' 的登录状态', 'ok');
    } else {
      setStatus(data.error || '同步失败', 'err');
    }
  } catch (e) {
    setStatus(e.message, 'err');
  }
});

// --- 配置管理（打开扩展自带的管理页面）---
manageBtn.addEventListener('click', () => {
  chrome.tabs.create({ url: chrome.runtime.getURL('manage.html') });
});

// --- 管理后台（超管）---
adminBtn.addEventListener('click', () => {
  chrome.tabs.create({ url: chrome.runtime.getURL('admin/index.html') });
});

// --- AI 浏览器控制（通过 background service worker）---

// 查询 background 获取当前 CDP 状态并更新 UI
async function updateCdpUI() {
  try {
    const status = await chrome.runtime.sendMessage({ action: 'cdpStatus' });
    if (status.active) {
      cdpBtn.textContent = '停止AI控制';
      cdpBtn.style.background = '#e74c3c';
      cdpBtn.style.color = '#fff';
      setStatus('AI 浏览器控制已开启', 'ok');
    } else {
      resetCdpBtn();
    }
  } catch {
    resetCdpBtn();
  }
}

function resetCdpBtn() {
  cdpBtn.textContent = '授权AI控制当前标签页';
  cdpBtn.style.background = '';
  cdpBtn.style.color = '';
}

cdpBtn.addEventListener('click', async () => {
  setStatus('处理中...', '');
  try {
    const result = await chrome.runtime.sendMessage({ action: 'cdpToggle' });
    if (result.active) {
      cdpBtn.textContent = '停止AI控制';
      cdpBtn.style.background = '#e74c3c';
      cdpBtn.style.color = '#fff';
      setStatus('AI 浏览器控制已开启', 'ok');
    } else if (result.error) {
      resetCdpBtn();
      // 如果错误是 debugger 已附加，提示用户清理
      if (result.error.includes('already attached') || result.error.includes('debugger')) {
        setStatus('调试器残留，正在清理...', 'err');
        await chrome.runtime.sendMessage({ action: 'cdpForceCleanup' });
        setStatus('已清理，请重试', '');
      } else {
        setStatus(result.error, 'err');
      }
    } else {
      resetCdpBtn();
      setStatus('已断开AI控制', '');
    }
  } catch (e) {
    resetCdpBtn();
    setStatus('通信失败: ' + e.message, 'err');
  }
});

// --- 自动检测登录状态 ---
document.addEventListener('DOMContentLoaded', async () => {
  const base = await getServerUrl();
  if (base) serverUrlInput.value = base;

  if (!base) { showLogin(); return; }

  try {
    const data = await apiJSON('GET', '/api/csrf');
    if (data.success) {
      showMain('');  // 已有 session，跳转主界面
      return;
    }
  } catch {}

  showLogin();
});
