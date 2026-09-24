import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formatEvent, parseEvent } from './events';

test('parseEvent reads a synchro JSON line', () => {
  const event = parseEvent('{"event":"upload","level":"info","path":"src/app.go","time":"2026-09-24T09:36:04+02:00"}');
  assert.equal(event?.event, 'upload');
  assert.ok(event && event.event === 'upload' && event.path === 'src/app.go');
});

test('parseEvent ignores non-event lines', () => {
  assert.equal(parseEvent('  synchro dev — sync local files'), undefined);
  assert.equal(parseEvent('{not json'), undefined);
  assert.equal(parseEvent('{"foo":1}'), undefined);
});

test('formatEvent renders a readable line', () => {
  const line = formatEvent({ event: 'syncAllDone', level: 'info', uploaded: 3, total: 4, time: '2026-09-24T09:36:04Z' });
  assert.match(line, /^\d\d:\d\d:\d\d ⟳ Full sync complete: 3\/4 files uploaded\.$/);
});

test('formatEvent survives a missing exclude list', () => {
  const line = formatEvent({ event: 'watching', level: 'info', local: '.', remote: '/r', exclude: null, time: 'x' });
  assert.equal(line, '--:--:-- ◉ Watching . → /r');
});
