// PicoAide 配置管理 SPA 路由器

const $ = s => document.querySelector(s);
const $$ = s => document.querySelectorAll(s);

let currentUser = '';
const sections = Array.from($$('.tabs a')).map(a => a.dataset.section);
const defaultSection = 'channels';

function getSectionFromPath() {
  const path = window.location.pathname.replace(/\/+$/, '');
  const section = path.split('/').filter(Boolean)[1];
  return sections.includes(section) ? section : defaultSection;
}

function showManage(username) {
  $('#manage-layout').classList.remove('hidden');
  $('#user-display').textContent = username;
  currentUser = username;
}

$('#logout-btn').addEventListener('click', async () => {
  try { await api('POST', '/api/logout'); } catch {}
  currentUser = '';
  window.location.href = '/login';
});

let currentSection = '';

async function navigate(section) {
  if (!sections.includes(section)) section = defaultSection;
  if (currentSection === section) return;
  currentSection = section;

  $$('.tab').forEach(a => a.classList.toggle('active', a.dataset.section === section));

  history.replaceState(null, '', '/manage/' + section);

  try {
    const resp = await fetch('templates/' + section + '.html');
    $('#content-area').innerHTML = await resp.text();
    const mod = await import('./modules/' + section + '.js');
    mod.init({ Api, esc, showMsg, $, $$, getCSRF, confirmModal, alertModal, promptModal, showModal });
  } catch (e) {
    $('#content-area').innerHTML = '<div class="card"><p>加载失败: ' + esc(e.message) + '</p></div>';
  }
}

document.addEventListener('DOMContentLoaded', async () => {
  const base = await getServerUrl();
  if (!base) { window.location.href = '/login'; return; }

  try {
    const info = await apiJSON('GET', '/api/user/info');
    if (info.success) {
      if (info.role === 'superadmin') {
        window.location.href = '/admin/dashboard';
        return;
      }
      showManage(info.username || 'user');

      if (info.unified_auth) {
        const tabBtn = document.getElementById('tab-password-btn');
        if (tabBtn) tabBtn.classList.add('hidden');
      }

      navigate(getSectionFromPath());
      return;
    }
  } catch {}
  window.location.href = '/login';
});

window.addEventListener('popstate', () => {
  navigate(getSectionFromPath());
});
