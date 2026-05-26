export function init({ Api, esc, showMsg }) {
  const statusCard = document.getElementById('tls-status-card');
  const uploadCard = document.getElementById('tls-upload-card');
  if (!statusCard || !uploadCard) return;

  const enabledLabel = document.getElementById('tls-enabled-label');
  const subjectEl = document.getElementById('tls-subject');
  const issuerEl = document.getElementById('tls-issuer');
  const notAfterEl = document.getElementById('tls-not-after');
  const certInput = document.getElementById('cert-pem-input');
  const keyInput = document.getElementById('key-pem-input');
  const uploadBtn = document.getElementById('tls-upload-btn');
  const clearBtn = document.getElementById('tls-clear-btn');

  async function loadStatus() {
    try {
      const resp = await Api('GET', '/api/admin/tls/status');
      if (!resp.data) { enabledLabel.textContent = '加载失败'; return; }
      const d = resp.data;
      if (d.enabled && d.has_cert) {
        enabledLabel.innerHTML = '<span style="color:var(--green)">已启用</span>';
      } else if (d.has_cert) {
        enabledLabel.innerHTML = '<span style="color:var(--orange)">未启用</span>';
      } else {
        enabledLabel.textContent = '未配置';
      }
      subjectEl.textContent = d.subject || '-';
      issuerEl.textContent = d.issuer || '-';
      if (d.not_after) {
        const date = new Date(d.not_after);
        notAfterEl.textContent = date.toLocaleDateString('zh-CN', { year:'numeric', month:'long', day:'numeric' });
        if (d.expired) {
          notAfterEl.innerHTML += ' <span style="color:var(--red)">（已过期）</span>';
        }
      }
    } catch (e) {
      enabledLabel.textContent = '查询失败';
    }
  }

  uploadBtn.addEventListener('click', async () => {
    const cert = certInput.value.trim();
    const key = keyInput.value.trim();
    if (!cert || !key) {
      showMsg('tls-msg', '请填写证书和私钥', 'error');
      return;
    }
    const form = new FormData();
    form.append('cert_pem', cert);
    form.append('key_pem', key);
    try {
      const resp = await Api('POST', '/api/admin/tls/upload', form, true);
      showMsg('tls-msg', resp.message || '证书已保存', resp.success ? 'success' : 'error');
      if (resp.success) {
        loadStatus();
        certInput.value = '';
        keyInput.value = '';
      }
    } catch (e) {
      showMsg('tls-msg', '上传失败: ' + e.message, 'error');
    }
  });

  clearBtn.addEventListener('click', async () => {
    try {
      const resp = await Api('POST', '/api/admin/tls/clear');
      showMsg('tls-msg', resp.message || '证书已清除', resp.success ? 'success' : 'error');
      if (resp.success) loadStatus();
    } catch (e) {
      showMsg('tls-msg', '清除失败: ' + e.message, 'error');
    }
  });

  loadStatus();
}
