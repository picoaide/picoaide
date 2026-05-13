/* PicoAide 公共方法 */

function getServerUrl() {
  return Promise.resolve(window.location.origin.replace(/\/+$/, ''));
}

function api(method, path, opts) {
  opts = opts || {};
  return getServerUrl().then(base => {
    if (!base) throw new Error('未设置服务器地址');
    return fetch(base + path, Object.assign({ method: method, credentials: 'include' }, opts));
  });
}

function apiJSON(method, path, opts) {
  return api(method, path, opts).then(r => r.json());
}

function getCSRF() {
  return apiJSON('GET', '/api/csrf').then(d => {
    if (!d.success) throw new Error('获取 CSRF token 失败');
    return d.csrf_token;
  });
}

function formBody(obj) {
  return Object.keys(obj).map(k => encodeURIComponent(k) + '=' + encodeURIComponent(obj[k])).join('&');
}

function esc(s) {
  var d = document.createElement('div');
  d.textContent = String(s);
  return d.innerHTML;
}

var Api = {
  get: function(path) { return apiJSON('GET', path); },
  post: function(path, params) {
    return getCSRF().then(function(csrf) {
      return apiJSON('POST', path, {
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: formBody(Object.assign({}, params, { csrf_token: csrf })),
      });
    });
  },
};

function showMsg(el, text, ok) {
  if (typeof el === 'string') el = document.querySelector(el);
  if (!el) return;
  el.textContent = text || '';
  el.className = text ? ('msg ' + (ok ? 'msg-ok' : 'msg-err')) : 'msg';
  if (text) setTimeout(function() { el.textContent = ''; el.className = 'msg'; }, 4000);
}

// 模态框组件：showModal({title, body, footer, width}) → Promise<buttonValue>
// body: HTML string
// footer: [{label, value, primary, danger}]
// 返回 Promise，resolve(buttonValue) 或 reject('cancel')
function showModal(opts) {
  var prev = document.querySelector('.modal-overlay');
  if (prev) prev.remove();
  return new Promise(function(resolve, reject) {
    var overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    var footerHtml = '';
    if (opts.footer) {
      footerHtml = '<div class="modal-footer">' + opts.footer.map(function(b) {
        var cls = 'btn btn-sm';
        if (b.primary) cls += ' btn-primary';
        else if (b.danger) cls += ' btn-danger';
        else cls += ' btn-outline';
        return '<button class="' + cls + '" data-value="' + esc(b.value) + '">' + esc(b.label) + '</button>';
      }).join('') + '</div>';
    }
    var widthStyle = opts.width ? ' style="max-width:' + opts.width + '"' : '';
    overlay.innerHTML =
      '<div class="modal"' + widthStyle + '>' +
        '<div class="modal-header">' + esc(opts.title || '') + '<button class="modal-close-btn">&times;</button></div>' +
        '<div class="modal-body">' + (opts.body || '') + '</div>' +
        footerHtml +
      '</div>';
    document.body.appendChild(overlay);

    function close(val) {
      if (val !== undefined) resolve(val);
      else reject('cancel');
      // 延迟移除，避免 Promise then 回调里读不到表单元素
      setTimeout(function() { overlay.remove(); }, 0);
    }

    overlay.querySelector('.modal-close-btn').addEventListener('click', function() { close(); });
    overlay.addEventListener('click', function(e) {
      if (e.target === overlay) close();
    });
    overlay.querySelectorAll('.modal-footer .btn[data-value]').forEach(function(btn) {
      btn.addEventListener('click', function() { close(btn.dataset.value); });
    });
    document.addEventListener('keydown', function handler(e) {
      if (e.key === 'Escape') { close(); document.removeEventListener('keydown', handler); }
    });
  });
}

function confirmModal(msg) {
  return showModal({
    title: '确认操作',
    body: '<p style="margin:0;font-size:.95rem;line-height:1.6">' + msg + '</p>',
    footer: [
      { label: '取消', value: 'false' },
      { label: '确定', value: 'true', primary: true }
    ]
  }).then(function(v) { return v === 'true'; }).catch(function() { return false; });
}

function alertModal(msg) {
  return showModal({
    title: '提示',
    body: '<p style="margin:0;font-size:.95rem;line-height:1.6">' + msg + '</p>',
    footer: [
      { label: '确定', value: 'true', primary: true }
    ]
  }).catch(function() {});
}

function promptModal(msg, defaultValue) {
  var prev = document.querySelector('.modal-overlay');
  if (prev) prev.remove();
  return new Promise(function(resolve) {
    var overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.innerHTML =
      '<div class="modal" style="max-width:440px">' +
        '<div class="modal-header">输入<button class="modal-close-btn">&times;</button></div>' +
        '<div class="modal-body" style="font-size:.95rem;line-height:1.6">' +
          '<p style="margin:0 0 8px">' + msg + '</p>' +
          '<input type="text" id="modal-prompt-input" value="' + esc(defaultValue || '') + '" style="width:100%;box-sizing:border-box">' +
        '</div>' +
        '<div class="modal-footer">' +
          '<button class="btn btn-outline btn-sm modal-cancel-btn">取消</button>' +
          '<button class="btn btn-primary btn-sm modal-ok-btn">确定</button>' +
        '</div>' +
      '</div>';
    document.body.appendChild(overlay);
    var input = overlay.querySelector('#modal-prompt-input');
    input.focus();
    input.select();
    function cleanup(val) {
      overlay.remove();
      resolve(val);
    }
    overlay.querySelector('.modal-close-btn').addEventListener('click', function() { cleanup(null); });
    overlay.querySelector('.modal-cancel-btn').addEventListener('click', function() { cleanup(null); });
    overlay.querySelector('.modal-ok-btn').addEventListener('click', function() { cleanup(input.value); });
    overlay.addEventListener('click', function(e) { if (e.target === overlay) cleanup(null); });
    input.addEventListener('keydown', function(e) { if (e.key === 'Enter') cleanup(input.value); });
  });
}
