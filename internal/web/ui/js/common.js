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
      overlay.remove();
      if (val !== undefined) resolve(val);
      else reject('cancel');
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
