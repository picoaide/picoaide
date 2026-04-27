(function () {
  // ── Native toString masking ──
  // Make all injected getters appear as native code when inspected
  const origToString = Function.prototype.toString;
  const nativeMap = new WeakMap();

  Function.prototype.toString = function () {
    return nativeMap.has(this) ? nativeMap.get(this) : origToString.call(this);
  };
  nativeMap.set(Function.prototype.toString, 'function toString() { [native code] }');

  function mask(fn, name) {
    nativeMap.set(fn, 'function get ' + name + '() { [native code] }');
    return fn;
  }

  function defProp(obj, prop, value) {
    const getter = mask(typeof value === 'function' ? value : () => value, prop);
    Object.defineProperty(obj, prop, { get: getter, configurable: true, enumerable: true });
  }

  // ── PRNG ──
  const NOISE_SEED = typeof INSTANCE_SEED !== 'undefined' ? INSTANCE_SEED : 0x4D616331;
  function mulberry32(a) {
    return function () {
      a |= 0; a = a + 0x6D2B79F5 | 0;
      var t = Math.imul(a ^ a >>> 15, 1 | a);
      t = t + Math.imul(t ^ t >>> 7, 61 | t) ^ t;
      return ((t ^ t >>> 14) >>> 0) / 4294967296;
    };
  }

  // ── Dynamic Chrome version ──
  var origUA = navigator.userAgent;
  var verMatch = origUA.match(/Chrome\/([\d.]+)/);
  var fullVer = verMatch ? verMatch[1] : '136.0.0.0';
  var major = fullVer.split('.')[0];

  var spoofedUA =
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) ' +
    'AppleWebKit/537.36 (KHTML, like Gecko) ' +
    'Chrome/' + fullVer + ' Safari/537.36';

  // ── Navigator properties ──
  defProp(navigator, 'userAgent', spoofedUA);
  defProp(navigator, 'platform', 'MacIntel');
  defProp(navigator, 'appVersion',
    '5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/' + fullVer + ' Safari/537.36');
  defProp(navigator, 'vendor', 'Google Inc.');
  defProp(navigator, 'language', 'zh-CN');
  defProp(navigator, 'languages', ['zh-CN', 'zh', 'en-US', 'en']);
  defProp(navigator, 'hardwareConcurrency', 8);
  defProp(navigator, 'deviceMemory', 8);
  defProp(navigator, 'maxTouchPoints', 0);
  defProp(navigator, 'productSub', '20030107');
  defProp(navigator, 'product', 'Gecko');

  // ── userAgentData ──
  if ('userAgentData' in navigator) {
    var brands = [
      { brand: 'Chromium', version: major },
      { brand: 'Google Chrome', version: major },
      { brand: 'Not-A.Brand', version: '99' }
    ];
    var uaData = {
      brands: brands,
      mobile: false,
      platform: 'macOS',
      getHighEntropyValues: function () {
        return Promise.resolve({
          architecture: 'arm',
          bitness: '64',
          brands: brands,
          fullVersionList: [
            { brand: 'Chromium', version: fullVer },
            { brand: 'Google Chrome', version: fullVer }
          ],
          mobile: false,
          model: '',
          platform: 'macOS',
          platformVersion: '15.3.0',
          uaFullVersion: fullVer,
          wow64: false
        });
      },
      toJSON: function () { return uaData; }
    };
    // Mask all methods in uaData
    mask(uaData.getHighEntropyValues, 'getHighEntropyValues');
    mask(uaData.toJSON, 'toJSON');
    defProp(navigator, 'userAgentData', uaData);
  }

  // ── plugins ──
  var fakePlugins = [
    { name: 'PDF Viewer', filename: 'internal-pdf-viewer' },
    { name: 'Chrome PDF Viewer', filename: 'internal-pdf-viewer' },
    { name: 'Chromium PDF Viewer', filename: 'internal-pdf-viewer' },
    { name: 'Microsoft Edge PDF Viewer', filename: 'internal-pdf-viewer' },
    { name: 'WebKit built-in PDF', filename: 'internal-pdf-viewer' }
  ];
  var pluginArray = Object.create(PluginArray.prototype);
  fakePlugins.forEach(function (p, i) {
    var plugin = Object.create(Plugin.prototype);
    Object.defineProperty(plugin, 'name', { get: function () { return p.name; } });
    Object.defineProperty(plugin, 'filename', { get: function () { return p.filename; } });
    Object.defineProperty(plugin, 'description', { get: function () { return ''; } });
    Object.defineProperty(plugin, 'length', { get: function () { return 0; } });
    pluginArray[i] = plugin;
  });
  Object.defineProperty(pluginArray, 'length', { get: function () { return fakePlugins.length; } });
  pluginArray.item = mask(function (i) { return pluginArray[i]; }, 'item');
  pluginArray.namedItem = mask(function (n) { return null; }, 'namedItem');
  pluginArray.refresh = mask(function () { }, 'refresh');
  defProp(navigator, 'plugins', pluginArray);

  // ── mimeTypes ──
  var fakeMime = Object.create(MimeTypeArray.prototype);
  Object.defineProperty(fakeMime, 'length', { get: function () { return 2; } });
  ['application/pdf', 'text/pdf'].forEach(function (type, i) {
    var m = Object.create(MimeType.prototype);
    Object.defineProperty(m, 'type', { get: function () { return type; } });
    fakeMime[i] = m;
  });
  fakeMime.item = mask(function (i) { return fakeMime[i]; }, 'item');
  fakeMime.namedItem = mask(function (n) { return null; }, 'namedItem');
  defProp(navigator, 'mimeTypes', fakeMime);

  // ── webdriver: DO NOT override ──
  // --disable-blink-features=AutomationControlled handles this natively

  // ── screen ── MacBook Pro 16"
  var screenProps = { width: 2560, height: 1600, availWidth: 2560, availHeight: 1545, colorDepth: 30, pixelDepth: 30 };
  for (var key in screenProps) {
    defProp(screen, key, screenProps[key]);
  }

  // ── devicePixelRatio ──
  defProp(window, 'devicePixelRatio', 2);

  // ── window dimensions ──
  defProp(window, 'outerWidth', 2560);
  defProp(window, 'outerHeight', 1545);
  defProp(window, 'innerWidth', 2560);
  defProp(window, 'innerHeight', 1460);

  // ── timezone ──
  var _DateTimeFormat = Intl.DateTimeFormat;
  var maskedDTF = mask(function (locale, options) {
    options = options || {};
    if (!options.timeZone) options.timeZone = 'Asia/Shanghai';
    return new _DateTimeFormat(locale, options);
  }, 'DateTimeFormat');
  Intl.DateTimeFormat = maskedDTF;
  Intl.DateTimeFormat.prototype = _DateTimeFormat.prototype;
  Intl.DateTimeFormat.supportedLocalesOf = _DateTimeFormat.supportedLocalesOf;

  // ── WebGL ── Apple M1
  var hookGL = function (proto) {
    var orig = proto.getParameter;
    proto.getParameter = mask(function (param) {
      if (param === 37445) return 'Apple Inc.';
      if (param === 37446) return 'Apple M1';
      return orig.call(this, param);
    }, 'getParameter');
  };
  hookGL(WebGLRenderingContext.prototype);
  if (typeof WebGL2RenderingContext !== 'undefined') hookGL(WebGL2RenderingContext.prototype);

  // ── chrome object ──
  if (!window.chrome) {
    window.chrome = {
      app: { isInstalled: false, InstallState: {}, RunningState: {} },
      runtime: {},
      loadTimes: function () { return {}; },
      csi: function () { return {}; }
    };
  }

  // ── Notification ──
  if (typeof Notification !== 'undefined') {
    Object.defineProperty(Notification, 'permission', {
      get: mask(function () { return 'default'; }, 'permission'),
      configurable: true
    });
  }

  // ── document.hasFocus ──
  Document.prototype.hasFocus = mask(function () { return true; }, 'hasFocus');

  // ── Permissions ──
  if (navigator.permissions) {
    var origQuery = navigator.permissions.query.bind(navigator.permissions);
    navigator.permissions.query = mask(function (params) {
      if (params.name === 'notifications') return Promise.resolve({ state: 'prompt', onchange: null });
      return origQuery(params);
    }, 'query');
  }

  // ── CDP / cdc_ cleanup ──
  function removeCdcVars() {
    var changed = false;
    for (var k in window) {
      if (k.indexOf('cdc_') === 0) {
        delete window[k];
        changed = true;
      }
    }
    if (changed) console.log('[spoof] removed cdc_ variables');
  }
  removeCdcVars();
  setInterval(removeCdcVars, 3000);

  // ── Canvas noise ──
  var origFillText = CanvasRenderingContext2D.prototype.fillText;
  CanvasRenderingContext2D.prototype.fillText = mask(function (text, x, y, maxWidth) {
    var rng = mulberry32(NOISE_SEED ^ (text.length * 137));
    return origFillText.call(this, text, x + (rng() - 0.5) * 0.02, y + (rng() - 0.5) * 0.02, maxWidth);
  }, 'fillText');

  var origStrokeText = CanvasRenderingContext2D.prototype.strokeText;
  CanvasRenderingContext2D.prototype.strokeText = mask(function (text, x, y, maxWidth) {
    var rng = mulberry32(NOISE_SEED ^ (text.length * 251));
    return origStrokeText.call(this, text, x + (rng() - 0.5) * 0.02, y + (rng() - 0.5) * 0.02, maxWidth);
  }, 'strokeText');
})();
