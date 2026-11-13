import test from 'node:test';
import assert from 'node:assert/strict';
import { mergeTabs } from '../assets/tabs.js';

test('a second view of the same session replaces the first tab', () => {
  const run = { id: 'run-1', key: 'ses-1', session_id: 'ses-1', kind: 'run', href: '#/runs/run-1', title: 'Refactor upload retries' };
  const session = { id: 'ses-1', key: 'ses-1', session_id: 'ses-1', kind: 'session', href: '#/sessions/ses-1', title: 'Refactor upload retries' };
  const tabs = mergeTabs([run], session);
  assert.equal(tabs.length, 1);
  assert.equal(tabs[0].href, '#/sessions/ses-1');
});

test('an existing tab is focused by moving it to the front, not duplicated', () => {
  const first = { id: 'a', key: 'a', kind: 'session', href: '#/sessions/a', title: 'A' };
  const second = { id: 'b', key: 'b', kind: 'session', href: '#/sessions/b', title: 'B' };
  const tabs = mergeTabs([first, second], first);
  assert.deepEqual(tabs.map((tab) => tab.key), ['a', 'b']);
});

test('different sessions keep separate tabs and old shape duplicates collapse', () => {
  const a = { id: 'a', key: 'a', session_id: 'a', kind: 'session', href: '#/sessions/a', title: 'A' };
  const b = { id: 'run-b', key: 'b', session_id: 'b', kind: 'run', href: '#/runs/run-b', title: 'B' };
  const tabs = mergeTabs([a], b);
  assert.deepEqual(tabs.map((tab) => tab.key), ['b', 'a']);
});
