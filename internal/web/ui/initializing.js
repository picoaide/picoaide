var initTitle = document.getElementById('init-title');
var initDetail = document.getElementById('init-detail');
var initMsg = document.getElementById('init-msg');
var logoutBtn = document.getElementById('logout-btn');
var pollTimer = null;
var failureCount = 0;

function statusText(data) {
  if (!data.image_ready) return '正在绑定用户镜像';
  if (!data.has_config) return '正在生成用户配置';
  if (data.status !== 'running') return '正在启动用户容器';
  return '正在完成最后检查';
}

async function pollStatus() {
  try {
    var data = await apiJSON('GET', '/api/user/init-status');
    if (!data.success) {
      showMsg(initMsg, data.error || '无法读取初始化状态', false);
      return;
    }
    failureCount = 0;
    if (data.ready) {
      initTitle.textContent = '初始化完成';
      initDetail.textContent = '正在进入个人后台...';
      window.location.replace('/manage');
      return;
    }
    initTitle.textContent = statusText(data);
    initDetail.textContent = '当前容器状态：' + (data.status || '未初始化');
  } catch (err) {
    failureCount++;
    if (failureCount >= 3) {
      showMsg(initMsg, err.message, false);
    }
  }
}

logoutBtn.addEventListener('click', async function() {
  logoutBtn.disabled = true;
  try {
    await apiJSON('POST', '/api/logout', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formBody({}),
    });
  } catch {}
  window.location.replace('/login');
});

document.addEventListener('DOMContentLoaded', function() {
  pollStatus();
  pollTimer = setInterval(pollStatus, 3000);
});

window.addEventListener('beforeunload', function() {
  if (pollTimer) clearInterval(pollTimer);
});
