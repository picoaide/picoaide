// 从源码复制纯函数进行测试（浏览器扩展无模块系统）

const VENDORS = {
  openai: 'OpenAI', anthropic: 'Anthropic', google: 'Google',
  azure: 'Azure', deepseek: 'DeepSeek', custom: '自定义',
};

function deepGet(obj, path) {
  var keys = path.split('.');
  var cur = obj;
  for (var i = 0; i < keys.length; i++) {
    if (cur == null) return undefined;
    var key = keys[i];
    if (key === '__proto__' || key === 'constructor' || key === 'prototype') return undefined;
    cur = cur[key];
  }
  return cur;
}

function deepSet(obj, path, value) {
  var keys = path.split('.');
  var cur = obj;
  for (var i = 0; i < keys.length - 1; i++) {
    var key = keys[i];
    if (key === '__proto__' || key === 'constructor' || key === 'prototype') return;
    if (cur[key] == null) cur[key] = {};
    cur = cur[key];
  }
  var last = keys[keys.length - 1];
  if (last === '__proto__' || last === 'constructor' || last === 'prototype') return;
  if (value !== '' && !isNaN(value) && String(Number(value)) === value) cur[last] = Number(value);
  else cur[last] = value;
}

function parseModelField(model) {
  if (!model) return { vendor: 'custom', modelId: '' };
  var slash = model.indexOf('/');
  if (slash === -1) return { vendor: 'custom', modelId: model };
  var v = model.substring(0, slash);
  var id = model.substring(slash + 1);
  if (VENDORS[v]) return { vendor: v, modelId: id };
  return { vendor: 'custom', modelId: model };
}

function buildModelField(vendor, modelId) {
  if (vendor === 'custom') return modelId;
  return vendor + '/' + modelId;
}

import { describe, it } from 'node:test';
import assert from 'node:assert/strict';

describe('deepGet', () => {
  it('should get top-level value', () => {
    assert.equal(deepGet({ a: 1 }, 'a'), 1);
  });

  it('should get nested value', () => {
    assert.equal(deepGet({ a: { b: { c: 42 } } }, 'a.b.c'), 42);
  });

  it('should return undefined for missing path', () => {
    assert.equal(deepGet({ a: 1 }, 'b'), undefined);
  });

  it('should return undefined for null intermediate', () => {
    assert.equal(deepGet({ a: null }, 'a.b'), undefined);
  });

  it('should block __proto__ access', () => {
    assert.equal(deepGet({}, '__proto__'), undefined);
  });

  it('should block constructor access', () => {
    assert.equal(deepGet({}, 'constructor'), undefined);
  });

  it('should block prototype access', () => {
    assert.equal(deepGet({}, 'prototype'), undefined);
  });

  it('should block nested __proto__', () => {
    assert.equal(deepGet({ a: {} }, 'a.__proto__.polluted'), undefined);
  });
});

describe('deepSet', () => {
  it('should set top-level value', () => {
    const obj = {};
    deepSet(obj, 'name', 'test');
    assert.equal(obj.name, 'test');
  });

  it('should set nested value creating intermediate objects', () => {
    const obj = {};
    deepSet(obj, 'a.b.c', 'deep');
    assert.equal(obj.a.b.c, 'deep');
  });

  it('should convert numeric strings to numbers', () => {
    const obj = {};
    deepSet(obj, 'count', '42');
    assert.equal(obj.count, 42);
    assert.ok(typeof obj.count === 'number');
  });

  it('should keep non-numeric strings as strings', () => {
    const obj = {};
    deepSet(obj, 'name', 'hello');
    assert.equal(obj.name, 'hello');
    assert.ok(typeof obj.name === 'string');
  });

  it('should keep empty string as string', () => {
    const obj = {};
    deepSet(obj, 'name', '');
    assert.equal(obj.name, '');
  });

  it('should block __proto__ set', () => {
    const obj = {};
    deepSet(obj, '__proto__.polluted', 'yes');
    assert.notEqual({}.polluted, 'yes');
  });

  it('should block constructor set', () => {
    const obj = {};
    deepSet(obj, 'constructor', 'evil');
    assert.equal(obj.constructor, Object);
  });

  it('should block prototype set', () => {
    const obj = {};
    deepSet(obj, 'prototype', 'evil');
    assert.equal(obj.prototype, undefined);
  });

  it('should block nested __proto__ set', () => {
    const obj = { a: {} };
    deepSet(obj, 'a.__proto__.polluted', 'yes');
    assert.notEqual({}.polluted, 'yes');
  });
});

describe('parseModelField', () => {
  it('should parse vendor/model format', () => {
    const result = parseModelField('openai/gpt-4');
    assert.equal(result.vendor, 'openai');
    assert.equal(result.modelId, 'gpt-4');
  });

  it('should return custom for no slash', () => {
    const result = parseModelField('my-model');
    assert.equal(result.vendor, 'custom');
    assert.equal(result.modelId, 'my-model');
  });

  it('should return custom for unknown vendor', () => {
    const result = parseModelField('unknown/model');
    assert.equal(result.vendor, 'custom');
    assert.equal(result.modelId, 'unknown/model');
  });

  it('should handle empty input', () => {
    const result = parseModelField('');
    assert.equal(result.vendor, 'custom');
    assert.equal(result.modelId, '');
  });

  it('should handle null/undefined', () => {
    const result = parseModelField(null);
    assert.equal(result.vendor, 'custom');
    assert.equal(result.modelId, '');
  });

  it('should parse all known vendors', () => {
    for (const v of Object.keys(VENDORS)) {
      if (v === 'custom') continue;
      const result = parseModelField(v + '/test-model');
      assert.equal(result.vendor, v);
      assert.equal(result.modelId, 'test-model');
    }
  });
});

describe('buildModelField', () => {
  it('should build vendor/model for known vendors', () => {
    assert.equal(buildModelField('openai', 'gpt-4'), 'openai/gpt-4');
  });

  it('should return modelId only for custom', () => {
    assert.equal(buildModelField('custom', 'my-model'), 'my-model');
  });
});
