export async function init(ctx) {
  const { Api, showMsg, $ } = ctx;
  var currentStatus = {};

  await loadStatus();

  $('#tls-enabled').addEventListener('change', function() {
    $('#cert-upload-card').style.display = this.value === 'true' ? 'block' : 'none';
  });

  $('#save-btn').addEventListener('click', saveConfig);
  $('#reset-btn').addEventListener('click', async () => {
    if (await confirmModal('重新加载？未保存的修改将丢失。')) loadStatus();
  });

  async function loadStatus() {
    showMsg('#tls-msg', '加载中...', true);
    try {
      var res = await Api.get('/api/admin/tls/status');
      if (!res.success) { showMsg('#tls-msg', res.error, false); return; }
      currentStatus = res;
      renderStatus();
      showMsg('#tls-msg', '');
    } catch (e) { showMsg('#tls-msg', e.message, false); }
  }

  function renderStatus() {
    var el = $('#tls-status');
    if (currentStatus.configured && currentStatus.cert_info) {
      var info = currentStatus.cert_info;
      el.innerHTML =
        '<div class="grid-2">' +
        '  <div>HTTPS: ' + (currentStatus.enabled ? '已启用' : '未启用') + '</div>' +
        '  <div>域名: ' + esc(info.subject) + '</div>' +
        '  <div>颁发者: ' + esc(info.issuer) + '</div>' +
        '  <div>过期: ' + esc(formatDate(info.not_after)) + '</div>' +
        '  <div>SANs: ' + esc((info.sans || []).join(', ')) + '</div>' +
        '</div>';
      $('#current-cert-info').textContent = '当前证书: ' + info.subject + '，更换证书请重新选择文件';
    } else {
      el.innerHTML = '<div class="text-sm text-muted">尚未配置证书</div>';
      $('#current-cert-info').textContent = '';
    }
    $('#tls-enabled').value = currentStatus.enabled ? 'true' : 'false';
    $('#cert-upload-card').style.display = currentStatus.enabled ? 'block' : 'none';
  }

  async function saveConfig() {
    var enabled = $('#tls-enabled').value === 'true';

    if (enabled) {
      var certFile = $('#cert-file').files && $('#cert-file').files[0];
      var keyFile = $('#key-file').files && $('#key-file').files[0];

      if (!currentStatus.configured && !certFile) {
        showMsg('#tls-msg', '请选择证书文件', false);
        return;
      }
      if (!currentStatus.configured && !keyFile) {
        showMsg('#tls-msg', '请选择私钥文件', false);
        return;
      }
    }

    showMsg('#tls-msg', '保存中...', true);

    try {
      var csrf = await getCSRF();
      var form = new FormData();
      form.append('enabled', enabled ? 'true' : 'false');
      form.append('csrf_token', csrf);
      if (enabled && $('#cert-file').files[0]) form.append('cert', $('#cert-file').files[0]);
      if (enabled && $('#key-file').files[0]) form.append('key', $('#key-file').files[0]);

      var base = await getServerUrl();
      var resp = await fetch(base + '/api/admin/tls/upload', {
        method: 'POST',
        credentials: 'include',
        body: form,
      });
      var res = await resp.json();
      showMsg('#tls-msg', res.message || res.error, !!res.success);
      if (res.success) {
        currentStatus.enabled = enabled;
        if (res.cert_info) {
          currentStatus.configured = true;
          currentStatus.cert_info = res.cert_info;
        } else {
          currentStatus.configured = false;
          currentStatus.cert_info = null;
        }
        renderStatus();
      }
    } catch (e) { showMsg('#tls-msg', e.message, false); }
  }

  function esc(s) {
    if (s == null) return '';
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function formatDate(iso) {
    if (!iso) return '';
    return new Date(iso).toLocaleString('zh-CN');
  }

  async function getCSRF() {
    var res = await Api.get('/api/csrf');
    return res.csrf_token || '';
  }

  async function getServerUrl() {
    return window.location.origin.replace(/\/+$/, '');
  }
}
