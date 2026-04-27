// Modify HTTP Client Hints headers to match macOS Google Chrome

const ua = navigator.userAgent;
const match = ua.match(/Chrome\/([\d.]+)/);
const fullVer = match ? match[1] : '136.0.0.0';
const major = fullVer.split('.')[0];

const secChUa = `"Chromium";v="${major}", "Google Chrome";v="${major}", "Not/A)Brand";v="8"`;
const secChUaFull = `"Chromium";v="${fullVer}", "Google Chrome";v="${fullVer}", "Not/A)Brand";v="8"`;

const macUA = `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${fullVer} Safari/537.36`;

const RESOURCE_TYPES = [
  'main_frame', 'sub_frame', 'stylesheet', 'script', 'image',
  'font', 'object', 'xmlhttprequest', 'media', 'other'
];

chrome.declarativeNetRequest.updateDynamicRules({
  addRules: [{
    id: 1,
    priority: 1,
    action: {
      type: 'modifyHeaders',
      requestHeaders: [
        { header: 'User-Agent', operation: 'set', value: macUA },
        { header: 'Sec-CH-UA', operation: 'set', value: secChUa },
        { header: 'Sec-CH-UA-Mobile', operation: 'set', value: '?0' },
        { header: 'Sec-CH-UA-Platform', operation: 'set', value: '"macOS"' },
        { header: 'Sec-CH-UA-Platform-Version', operation: 'set', value: '"15.3.0"' },
        { header: 'Sec-CH-UA-Full-Version-List', operation: 'set', value: secChUaFull },
        { header: 'Sec-CH-UA-Arch', operation: 'set', value: '"arm"' },
        { header: 'Sec-CH-UA-Bitness', operation: 'set', value: '"64"' },
        { header: 'Accept-Language', operation: 'set', value: 'zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7' }
      ]
    },
    condition: {
      urlFilter: '*',
      resourceTypes: RESOURCE_TYPES
    }
  }, {
    // Block web pages from accessing CDP debug port
    id: 2,
    priority: 2,
    action: { type: 'block' },
    condition: {
      urlFilter: '||127.0.0.1:9222',
      resourceTypes: ['xmlhttprequest', 'websocket', 'other', 'sub_frame']
    }
  }, {
    id: 3,
    priority: 2,
    action: { type: 'block' },
    condition: {
      urlFilter: '||localhost:9222',
      resourceTypes: ['xmlhttprequest', 'websocket', 'other', 'sub_frame']
    }
  }],
  removeRuleIds: [1, 2, 3]
});
