export async function init(ctx) {
  var $ = ctx.$, esc = ctx.esc, showMsg = ctx.showMsg, Api = ctx.Api, confirmModal = ctx.confirmModal;
  var savedEmail = null;

  async function loadConfig() {
    var data = await Api.get('/api/user/email');
    if (!data.success) {
      showMsg('#email-status', data.error || '加载失败', false);
      return;
    }
    if (data.configured && data.email) {
      savedEmail = data.email;
      showConfigCard(data.email);
    } else {
      savedEmail = null;
      showForm(null);
    }
  }

  function showConfigCard(email) {
    $('#email-form').style.display = 'none';
    $('#email-config-card').style.display = 'block';
    $('#email-save-btn').style.display = 'none';
    $('#email-delete-btn').style.display = 'inline-block';
    $('#card-email').textContent = esc(email.email);
    $('#card-smtp').textContent = email.smtpHost + ':' + email.smtpPort + (email.smtpTls ? ' (SSL)' : ' (STARTTLS)');
    $('#card-imap').textContent = email.imapHost + ':' + email.imapPort + (email.imapTls ? ' (SSL)' : ' (STARTTLS)');
    $('#card-status').textContent = email.enabled ? '已启用' : '未启用';
    $('#card-test').textContent = email.testResult || '—';
  }

  function showForm(email) {
    $('#email-form').style.display = 'block';
    $('#email-config-card').style.display = 'none';
    $('#email-save-btn').style.display = 'inline-block';
    $('#email-delete-btn').style.display = 'none';
    if (email) {
      $('#email-addr').value = email.email || '';
      $('#smtp-host').value = email.smtpHost || '';
      $('#smtp-port').value = email.smtpPort || 587;
      $('#smtp-tls').value = email.smtpTls ? 'true' : 'false';
      $('#imap-host').value = email.imapHost || '';
      $('#imap-port').value = email.imapPort || 993;
      $('#imap-tls').value = email.imapTls ? 'true' : 'false';
      $('#login-user').value = email.loginUser || '';
      $('#login-password').value = '';
    } else {
      $('#email-addr').value = '';
      $('#smtp-host').value = '';
      $('#smtp-port').value = 587;
      $('#smtp-tls').value = 'false';
      $('#imap-host').value = '';
      $('#imap-port').value = 993;
      $('#imap-tls').value = 'true';
      $('#login-user').value = '';
      $('#login-password').value = '';
    }
  }

  function getFormValues() {
    return {
      email: $('#email-addr').value.trim(),
      smtpHost: $('#smtp-host').value.trim(),
      smtpPort: parseInt($('#smtp-port').value) || 587,
      smtpTls: $('#smtp-tls').value === 'true',
      imapHost: $('#imap-host').value.trim(),
      imapPort: parseInt($('#imap-port').value) || 993,
      imapTls: $('#imap-tls').value === 'true',
      loginUser: $('#login-user').value.trim(),
      loginPassword: $('#login-password').value,
    };
  }

  async function testConnection() {
    var vals = getFormValues();
    if (!vals.email || !vals.smtpHost || !vals.imapHost || !vals.loginUser || !vals.loginPassword) {
      showMsg('#email-status', '请先填写所有必填字段', false);
      return;
    }
    $('#email-test-btn').disabled = true;
    $('#email-test-btn').textContent = '测试中...';
    try {
      var res = await Api.post('/api/user/email/test', vals);
      if (res.smtp && res.imap) {
        showMsg('#email-status', 'SMTP 和 IMAP 连接均成功 ✓', true);
      } else if (res.smtp) {
        showMsg('#email-status', 'SMTP 连接成功 ✓，IMAP 失败: ' + (res.error || '未知错误'), false);
      } else {
        showMsg('#email-status', 'SMTP 连接失败: ' + (res.error || '未知错误'), false);
      }
    } catch(e) {
      showMsg('#email-status', '测试失败: ' + e.message, false);
    } finally {
      $('#email-test-btn').disabled = false;
      $('#email-test-btn').textContent = '测试连接';
    }
  }

  async function saveConfig() {
    var vals = getFormValues();
    if (!vals.email || !vals.smtpHost || !vals.imapHost || !vals.loginUser || !vals.loginPassword) {
      showMsg('#email-status', '所有字段均为必填', false);
      return;
    }
    $('#email-save-btn').disabled = true;
    try {
      var res = await Api.post('/api/user/email', vals);
      if (res.success) {
        showMsg('#email-status', '配置已保存', true);
        loadConfig();
      } else {
        showMsg('#email-status', res.error || '保存失败', false);
      }
    } catch(e) {
      showMsg('#email-status', '保存失败: ' + e.message, false);
    } finally {
      $('#email-save-btn').disabled = false;
    }
  }

  async function deleteConfig() {
    var confirmed = await confirmModal('确定要删除邮箱配置吗？');
    if (!confirmed) return;
    var res = await Api.post('/api/user/email/delete', {});
    if (res.success) {
      showMsg('#email-status', '配置已删除', true);
      savedEmail = null;
      showForm(null);
    } else {
      showMsg('#email-status', res.error || '删除失败', false);
    }
  }

  $('#email-save-btn').addEventListener('click', saveConfig);
  $('#email-delete-btn').addEventListener('click', deleteConfig);
  $('#email-test-btn').addEventListener('click', testConnection);
  $('#email-edit-btn').addEventListener('click', function() {
    showForm(savedEmail);
  });

  loadConfig();
}
