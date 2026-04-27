// PicoAide 管理后台

const $ = s => document.querySelector(s);
const $$ = s => document.querySelectorAll(s);

let currentUser = '';

function showLogin() {
  $('#login-page').classList.remove('hidden');
  $('#admin-layout').classList.add('hidden');
  currentUser = '';
}

function showAdmin(username) {
  $('#login-page').classList.add('hidden');
  $('#admin-layout').classList.remove('hidden');
  $('#user-display').textContent = username;
  currentUser = username;
}

// 登录
$('#login-btn').addEventListener('click', async () => {
  const msg = $('#login-msg');
  msg.textContent = '';
  msg.className = 'msg';

  const url = $('#login-server').value.trim().replace(/\/+$/, '');
  if (!url) { showMsg(msg, '请输入服务器地址', false); return; }

  await chrome.storage.local.set({ serverUrl: url });

  try {
    const res = await apiJSON('POST', '/api/login', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formBody({ username: $('#login-user').value.trim(), password: $('#login-pass').value }),
    });
    if (!res.success) { showMsg(msg, res.error || '登录失败', false); return; }

    const info = await apiJSON('GET', '/api/user/info');
    if (!info.success || info.role !== 'superadmin') {
      showMsg(msg, '仅超级管理员可访问', false);
      return;
    }

    currentUser = res.username;
    $('#login-pass').value = '';
    showAdmin(res.username);
    navigate('dashboard');
  } catch (e) {
    showMsg(msg, e.message, false);
  }
});

// 退出
$('#logout-btn').addEventListener('click', async () => {
  try { await api('POST', '/api/logout'); } catch {}
  showLogin();
});

// 导航
let currentSection = '';

async function navigate(section) {
  if (currentSection === section) return;
  currentSection = section;

  $$('.sidebar-nav a').forEach(a => a.classList.toggle('active', a.dataset.section === section));

  try {
    const resp = await fetch('templates/' + section + '.html');
    $('#content-area').innerHTML = await resp.text();
    const mod = await import('./modules/' + section + '.js');
    mod.init({ Api, esc, showMsg, $, $$ });
  } catch (e) {
    $('#content-area').innerHTML = '<div class="card"><p>加载失败: ' + esc(e.message) + '</p></div>';
  }
}

$$('.sidebar-nav a').forEach(a => {
  a.addEventListener('click', e => { e.preventDefault(); navigate(a.dataset.section); });
});

// 自动登录
document.addEventListener('DOMContentLoaded', async () => {
  const base = await getServerUrl();
  $('#login-server').value = base;
  if (!base) { showLogin(); return; }

  try {
    const info = await apiJSON('GET', '/api/user/info');
    if (info.success && info.role === 'superadmin') {
      currentUser = info.username;
      showAdmin(info.username);
      navigate('dashboard');
      return;
    }
  } catch {}
  showLogin();
});
