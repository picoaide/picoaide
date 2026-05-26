var pendingCert = '';
var pendingKey = '';

export function init({ Api, esc, showMsg, confirmModal }) {
  const statusCard = document.getElementById('tls-status-card');
  const toggleCard = document.getElementById('tls-toggle-card');
  const uploadCard = document.getElementById('tls-upload-card');
  if (!statusCard || !toggleCard || !uploadCard) return;

  const enabledLabel = document.getElementById('tls-enabled-label');
  const subjectEl = document.getElementById('tls-subject');
  const sansEl = document.getElementById('tls-sans');
  const hostMatchEl = document.getElementById('tls-host-match');
  const issuerEl = document.getElementById('tls-issuer');
  const notAfterEl = document.getElementById('tls-not-after');

  const toggleInput = document.getElementById('tls-toggle-input');
  const toggleStatus = document.getElementById('tls-toggle-status');

  const certFileInput = document.getElementById('cert-file-input');
  const keyFileInput = document.getElementById('key-file-input');
  const verifyBtn = document.getElementById('tls-verify-btn');
  const verifyResult = document.getElementById('tls-verify-result');
  const saveBtn = document.getElementById('tls-save-btn');
  const clearBtn = document.getElementById('tls-clear-btn');

  // 验证结果各字段
  const vSubject = document.getElementById('verify-subject');
  const vSans = document.getElementById('verify-sans');
  const vHostMatch = document.getElementById('verify-host-match');
  const vIssuer = document.getElementById('verify-issuer');
  const vNotAfter = document.getElementById('verify-not-after');
  const vStatus = document.getElementById('verify-status');

  var currentEnabled = false;
  var hasCert = false;

  // ========== 加载状态 ==========
  async function loadStatus() {
    try {
      const resp = await Api.get('/api/admin/tls/status');
      if (!resp.success || !resp.data) {
        enabledLabel.textContent = '加载失败';
        return;
      }
      renderStatus(resp.data);
    } catch (e) {
      enabledLabel.textContent = '查询失败';
    }
  }

  function renderStatus(d) {
    currentEnabled = d.enabled;
    hasCert = d.has_cert;

    // HTTPS 状态
    if (d.enabled && d.has_cert) {
      enabledLabel.innerHTML = '<span style="color:var(--green)">已启用</span>';
    } else if (d.has_cert) {
      enabledLabel.innerHTML = '<span style="color:var(--orange)">未启用</span>';
    } else {
      enabledLabel.textContent = '未配置';
    }

    subjectEl.textContent = d.subject || '-';
    sansEl.textContent = d.sans ? d.sans.join(', ') : '-';

    if (d.host_match === true) {
      hostMatchEl.innerHTML = '<span style="color:var(--green)">&#10003; 匹配</span>';
    } else if (d.host_match === false) {
      hostMatchEl.innerHTML = '<span style="color:var(--red)">&#10007; 不匹配</span>';
    } else {
      hostMatchEl.textContent = '-';
    }

    issuerEl.textContent = d.issuer || '-';
    if (d.not_after) {
      const date = new Date(d.not_after);
      notAfterEl.textContent = date.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' });
      if (d.expired) {
        notAfterEl.innerHTML += ' <span style="color:var(--red)">（已过期）</span>';
      }
    }

    // Toggle 开关
    toggleInput.checked = d.enabled && d.has_cert;
    toggleStatus.textContent = d.enabled && d.has_cert ? '已启用' : (d.has_cert ? '已关闭' : '请先上传证书');
  }

  // ========== 文件选择 ==========
  function readFileAsText(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result);
      reader.onerror = () => reject(new Error('文件读取失败'));
      reader.readAsText(file);
    });
  }

  certFileInput.addEventListener('change', checkFilesReady);
  keyFileInput.addEventListener('change', checkFilesReady);

  function checkFilesReady() {
    verifyBtn.disabled = !certFileInput.files.length || !keyFileInput.files.length;
    verifyResult.style.display = 'none';
  }

  // ========== 验证证书 ==========
  verifyBtn.addEventListener('click', async () => {
    const certFile = certFileInput.files[0];
    const keyFile = keyFileInput.files[0];
    if (!certFile || !keyFile) return;

    try {
      pendingCert = await readFileAsText(certFile);
      pendingKey = await readFileAsText(keyFile);
    } catch (e) {
      showMsg('tls-msg', '文件读取失败: ' + e.message, 'error');
      return;
    }

    try {
      const resp = await Api.post('/api/admin/tls/verify', {
        cert_pem: pendingCert,
        key_pem: pendingKey,
      });
      if (!resp.success) {
        showMsg('tls-msg', resp.error || '证书验证失败', 'error');
        return;
      }
      showVerifyResult(resp.data);
    } catch (e) {
      showMsg('tls-msg', '验证失败: ' + e.message, 'error');
    }
  });

  function showVerifyResult(d) {
    vSubject.textContent = d.subject || '-';
    vSans.textContent = d.sans ? d.sans.join(', ') : '-';

    if (d.host_match === true) {
      vHostMatch.innerHTML = '<span style="color:var(--green)">&#10003; 匹配 (当前域名)</span>';
    } else if (d.host_match === false) {
      vHostMatch.innerHTML = '<span style="color:var(--red)">&#10007; 不匹配当前域名</span>';
    } else {
      vHostMatch.textContent = '-';
    }

    vIssuer.textContent = d.issuer || '-';
    if (d.not_after) {
      const date = new Date(d.not_after);
      vNotAfter.textContent = date.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' });
      if (d.expired) {
        vNotAfter.innerHTML += ' <span style="color:var(--red)">（已过期）</span>';
      }
    }

    if (d.expired) {
      vStatus.innerHTML = '<span style="color:var(--red)">&#10007; 证书已过期</span>';
    } else if (d.host_match === false) {
      vStatus.innerHTML = '<span style="color:var(--orange)">&#9888; 域名不匹配</span>';
    } else {
      vStatus.innerHTML = '<span style="color:var(--green)">&#10003; 证书有效</span>';
    }

    verifyResult.style.display = 'block';
  }

  // ========== 保存证书 ==========
  saveBtn.addEventListener('click', async () => {
    if (!pendingCert || !pendingKey) {
      showMsg('tls-msg', '请先验证证书', 'error');
      return;
    }
    try {
      const resp = await Api.post('/api/admin/tls/save', {
        cert_pem: pendingCert,
        key_pem: pendingKey,
        enabled: 'true',
      });
      showMsg('tls-msg', resp.message || '证书已保存', resp.success ? 'success' : 'error');
      if (resp.success) {
        pendingCert = '';
        pendingKey = '';
        certFileInput.value = '';
        keyFileInput.value = '';
        verifyResult.style.display = 'none';
        verifyBtn.disabled = true;
        await loadStatus();
      }
    } catch (e) {
      showMsg('tls-msg', '保存失败: ' + e.message, 'error');
    }
  });

  // ========== Toggle HTTPS ==========
  toggleInput.addEventListener('change', async () => {
    const enabled = toggleInput.checked;
    try {
      const resp = await Api.post('/api/admin/tls/toggle', { enabled: String(enabled) });
      if (resp.success) {
        toggleStatus.textContent = enabled ? '已启用' : '已关闭';
        showMsg('tls-msg', resp.message || 'HTTPS 开关已切换', 'success');
        await loadStatus();
      } else {
        toggleInput.checked = !enabled;
        showMsg('tls-msg', resp.error || '切换失败', 'error');
      }
    } catch (e) {
      toggleInput.checked = !enabled;
      showMsg('tls-msg', '切换失败: ' + e.message, 'error');
    }
  });

  // ========== 清除证书 ==========
  clearBtn.addEventListener('click', async () => {
    if (!await confirmModal('确定要清除证书吗？此操作将关闭 HTTPS。')) return;
    try {
      const resp = await Api.post('/api/admin/tls/clear');
      showMsg('tls-msg', resp.message || '证书已清除', resp.success ? 'success' : 'error');
      if (resp.success) {
        pendingCert = '';
        pendingKey = '';
        certFileInput.value = '';
        keyFileInput.value = '';
        verifyResult.style.display = 'none';
        verifyBtn.disabled = true;
        await loadStatus();
      }
    } catch (e) {
      showMsg('tls-msg', '清除失败: ' + e.message, 'error');
    }
  });

  // ========== 初始化 ==========
  loadStatus();
}
