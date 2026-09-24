import { test } from 'node:test';
import assert from 'node:assert/strict';
import { shortLabels, words } from './label';

test('words splits on separators and camelCase', () => {
  assert.deepEqual(words('ring-websites_cdh.web app'), ['ring', 'websites', 'cdh', 'web', 'app']);
  assert.deepEqual(words('pushNotificationsAPIConsumer'), ['push', 'Notifications', 'API', 'Consumer']);
});

test('shortLabels uses initials and keeps single words whole', () => {
  assert.deepEqual(
    shortLabels(['ring-websites-cdh-web', 'ring-websites-cdh-ingestion-worker-lambda', 'managerApi', 'api']),
    ['rwcw', 'rwciwl', 'ma', 'api'],
  );
});

test('shortLabels resolves collisions with more of the last word', () => {
  assert.deepEqual(shortLabels(['cdh-web', 'cdh-worker', 'cdhWebsite']), ['cdh-web', 'cwo', 'cwebs']);
  assert.deepEqual(shortLabels(['a-b', 'a_b']), ['a-b', 'a_b']);
});
