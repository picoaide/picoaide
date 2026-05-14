// 浏览器和平台检测
function detectBrowser() {
  var ua = navigator.userAgent;
  if (ua.indexOf('Edg') !== -1) return 'edge';
  if (ua.indexOf('Chrome') !== -1) return 'chrome';
  if (ua.indexOf('Firefox') !== -1) return 'firefox';
  if (ua.indexOf('Safari') !== -1) return 'safari';
  return 'other';
}

function detectOS() {
  var p = navigator.platform;
  if (p.indexOf('Win') !== -1) return 'windows';
  if (p.indexOf('Mac') !== -1) {
    if (navigator.userAgent.indexOf('ARM') !== -1) return 'mac-arm';
    return 'mac-intel';
  }
  if (p.indexOf('Linux') !== -1) return 'linux';
  return 'other';
}

function buildLink(href, label, opts) {
  var a = document.createElement('a');
  a.href = href;
  a.target = '_blank';
  a.className = 'btn btn-sm' + (opts && opts.primary ? ' btn-primary' : ' btn-outline');
  a.style.marginRight = '0.5em';
  a.style.marginBottom = '0.5em';
  a.textContent = label;
  return a;
}

export async function init(ctx) {
  var browser = detectBrowser();
  var os = detectOS();

  // 浏览器扩展链接
  var extContainer = ctx.$('#extension-links');
  if (browser === 'chrome') {
    extContainer.appendChild(buildLink(
      'https://chromewebstore.google.com/detail/nbmhmeodjpfmoldjomngknknakklebje?authuser=0&hl=zh-CN',
      '安装 Chrome 扩展', {primary: true}
    ));
  } else if (browser === 'edge') {
    var edgeBtn = buildLink('#', '安装 Edge 扩展', {primary: true});
    edgeBtn.style.opacity = '0.5';
    edgeBtn.style.pointerEvents = 'none';
    edgeBtn.title = '即将上线';
    extContainer.appendChild(edgeBtn);
  } else {
    extContainer.appendChild(buildLink(
      'https://chromewebstore.google.com/detail/nbmhmeodjpfmoldjomngknknakklebje?authuser=0&hl=zh-CN',
      '安装 Chrome 扩展', {primary: true}
    ));
  }

  // 桌面客户端链接
  var dtContainer = ctx.$('#desktop-links');
  var releaseUrl = 'https://github.com/picoaide/picoaide/releases/latest';

  if (os === 'windows') {
    dtContainer.appendChild(buildLink(releaseUrl, '下载 Windows 版', {primary: true}));
  } else if (os === 'mac-arm') {
    dtContainer.appendChild(buildLink(releaseUrl, '下载 macOS 版', {primary: true}));
  } else if (os === 'mac-intel') {
    dtContainer.appendChild(buildLink(releaseUrl, '下载 macOS 版', {primary: true}));
  }

  dtContainer.appendChild(buildLink(releaseUrl, '查看发布页', {}));
}
