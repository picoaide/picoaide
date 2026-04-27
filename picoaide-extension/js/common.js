/* PicoAide 公共方法 */

function getServerUrl() {
  return chrome.storage.local.get('serverUrl').then(r => (r.serverUrl || '').replace(/\/+$/, ''));
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
