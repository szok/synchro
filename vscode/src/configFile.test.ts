import { test } from 'node:test';
import assert from 'node:assert/strict';
import { withExclude } from './configFile';

test('withExclude appends a pattern and keeps the other fields', () => {
  const text = withExclude('{"host":"h","exclude":[".git"],"port":22}', 'node_modules');
  assert.equal(text, '{\n  "host": "h",\n  "exclude": [\n    ".git",\n    "node_modules"\n  ],\n  "port": 22\n}\n');
});

test('withExclude creates the list when missing', () => {
  assert.deepEqual(JSON.parse(withExclude('{"host":"h"}', 'dist') ?? ''), { host: 'h', exclude: ['dist'] });
});

test('withExclude leaves an existing pattern alone', () => {
  assert.equal(withExclude('{"exclude":["dist"]}', 'dist'), undefined);
});

test('withExclude rejects a non-object config', () => {
  assert.throws(() => withExclude('[]', 'dist'));
});
