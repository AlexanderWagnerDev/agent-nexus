import test from 'node:test';
import assert from 'node:assert/strict';
import { draftKey, parseStored } from '../assets/state.js';
import { backoffBase, backoffDelay, nextBackoff } from '../assets/ws.js';

test('parses stored JSON and falls back on missing or corrupt values', () => {
  assert.deepEqual(parseStored('{"a":1}', null), { a: 1 });
  assert.equal(parseStored('null', 'fallback'), 'fallback');
  assert.equal(parseStored('{broken', 'fallback'), 'fallback');
  assert.equal(parseStored(undefined, []).length, 0);
});

test('draft keys are scoped per run', () => {
  assert.equal(draftKey('run-1'), 'nexus-draft-run-1');
  assert.notEqual(draftKey('run-1'), draftKey('run-2'));
});

test('reconnect backoff grows and caps at 15 seconds', () => {
  assert.equal(backoffBase(1000), 1000);
  assert.equal(backoffBase(40000), 15000);
  assert.equal(nextBackoff(1000), 1700);
  assert.equal(nextBackoff(12000), 15000);
});

test('jittered delay stays inside the 80-120 percent window', () => {
  assert.equal(backoffDelay(1000, () => 0), 800);
  assert.equal(backoffDelay(1000, () => 1), 1200);
  assert.equal(backoffDelay(1000, () => 0.5), 1000);
  assert.equal(backoffDelay(40000, () => 1), 18000);
});
