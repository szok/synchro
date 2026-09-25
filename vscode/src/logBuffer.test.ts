import { test } from 'node:test';
import assert from 'node:assert/strict';
import { LogBuffer } from './logBuffer';

test('LogBuffer caps each folder separately', () => {
  const buffer = new LogBuffer(2);
  buffer.push('quiet', 'a');
  for (const n of [1, 2, 3]) {
    buffer.push(`busy ${n}`, 'b');
  }
  assert.deepEqual(
    buffer.all().map((l) => l.line),
    ['quiet', 'busy 2', 'busy 3'],
  );
});

test('LogBuffer merges folders in arrival order and clears one or all', () => {
  const buffer = new LogBuffer(10);
  buffer.push('a1', 'a');
  buffer.push('b1', 'b');
  buffer.push('raw');
  buffer.push('a2', 'a');
  assert.deepEqual(
    buffer.all().map((l) => [l.folder, l.line]),
    [
      ['a', 'a1'],
      ['b', 'b1'],
      [undefined, 'raw'],
      ['a', 'a2'],
    ],
  );
  assert.equal('folder' in buffer.all()[2], false);
  buffer.clear('a');
  assert.deepEqual(
    buffer.all().map((l) => l.line),
    ['b1', 'raw'],
  );
  buffer.clear();
  assert.deepEqual(buffer.all(), []);
});
