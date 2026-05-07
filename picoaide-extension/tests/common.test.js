// 从源码复制纯函数（浏览器扩展无模块系统）

function formBody(obj) {
  return Object.keys(obj).map(k => encodeURIComponent(k) + '=' + encodeURIComponent(obj[k])).join('&');
}

import { describe, it } from 'node:test';
import assert from 'node:assert/strict';

describe('formBody', () => {
  it('should encode simple object', () => {
    const result = formBody({ name: 'test', value: '123' });
    assert.ok(result.includes('name=test'));
    assert.ok(result.includes('value=123'));
  });

  it('should encode special characters', () => {
    const result = formBody({ query: 'hello world&more=yes' });
    assert.equal(result, 'query=hello%20world%26more%3Dyes');
  });

  it('should handle empty object', () => {
    assert.equal(formBody({}), '');
  });

  it('should handle unicode values', () => {
    const result = formBody({ name: '中文用户' });
    assert.ok(result.includes(encodeURIComponent('中文用户')));
  });

  it('should encode keys with special characters', () => {
    const result = formBody({ 'a b': 'c' });
    assert.equal(result, 'a%20b=c');
  });
});
