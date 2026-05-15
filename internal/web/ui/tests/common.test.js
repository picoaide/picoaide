import { describe, it } from 'node:test';
import assert from 'node:assert/strict';

// ============================================================
// 从 common.js 复制的纯函数
// ============================================================

function esc(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#x27;');
}

function formBody(obj) {
  return Object.keys(obj).map(k => encodeURIComponent(k) + '=' + encodeURIComponent(obj[k])).join('&');
}

// ============================================================
// esc 测试
// ============================================================

describe('esc', () => {
  it('should escape HTML special characters', () => {
    assert.equal(esc('<script>alert("xss")</script>'), '&lt;script&gt;alert(&quot;xss&quot;)&lt;/script&gt;');
  });

  it('should handle plain text unchanged', () => {
    assert.equal(esc('hello world'), 'hello world');
  });

  it('should escape ampersand', () => {
    assert.equal(esc('a&b'), 'a&amp;b');
  });

  it('should escape single quotes', () => {
    assert.equal(esc("it's"), 'it&#x27;s');
  });

  it('should convert non-string types', () => {
    assert.equal(esc(42), '42');
    assert.equal(esc(null), 'null');
    assert.equal(esc(undefined), 'undefined');
    assert.equal(esc(true), 'true');
  });
});

// ============================================================
// formBody 测试
// ============================================================

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

  it('should encode arrays and nested values', () => {
    const result = formBody({ items: ['a', 'b'] });
    assert.equal(result, 'items=a%2Cb');
  });
});
