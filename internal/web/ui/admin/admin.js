// PicoAide 管理后台

const $ = s => document.querySelector(s);
const $$ = s => document.querySelectorAll(s);

let currentUser = '';

async function getServerUrl() {
  return window.location.origin.replace(/\/+$/, '');
}

function showAdmin(username) {
  $('#admin-layout').classList.remove('hidden');
  $('#user-display').textContent = username;
  currentUser = username;
}

// 退出
$('#logout-btn').addEventListener('click', async () => {
  try { await api('POST', '/api/logout'); } catch {}
  window.location.href = '/login';
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
  if (!base) { window.location.href = '/login'; return; }

  try {
    const info = await apiJSON('GET', '/api/user/info');
    if (info.success && info.role === 'superadmin') {
      currentUser = info.username;
      showAdmin(info.username);
      navigate('dashboard');
      return;
    }
  } catch {}
  window.location.href = '/login';
});
