import { test } from 'node:test';
import assert from 'node:assert/strict';
import { eventEntry, formatEvent, parseEvent } from './events';

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
  const line = formatEvent({
    event: 'syncAllDone',
    level: 'info',
    uploaded: 3,
    total: 4,
    time: '2026-09-24T09:36:04Z',
  });
  assert.match(line, /^\d\d:\d\d:\d\d ⟳  \[syncAll\] Full sync complete: 3\/4 files uploaded\.$/);
});

test('formatEvent survives a missing exclude list', () => {
  const line = formatEvent({ event: 'watching', level: 'info', local: '.', remote: '/r', exclude: null, time: 'x' });
  assert.equal(line, '--:--:-- ◉  [watch] . → /r');
});

test('eventEntry colours events like the CLI', () => {
  const entry = eventEntry({ event: 'delete', level: 'info', path: 'a.go', time: 'x' });
  assert.deepEqual(entry, { time: '--:--:--', level: 'info', icon: '✕', color: 'red', tag: '[delete]', text: 'a.go' });
  assert.equal(eventEntry({ event: 'mkdir', level: 'info', path: 'd', time: 'x' }).color, 'blue');
  assert.equal(eventEntry({ event: 'connected', level: 'success', username: 'u', host: 'h', time: 'x' }).bold, true);
});
