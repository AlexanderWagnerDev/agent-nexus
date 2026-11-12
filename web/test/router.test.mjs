import test from 'node:test';
import assert from 'node:assert/strict';
import { parseRoute } from '../assets/routes.js';

test('routes the home and section pages', () => {
  assert.equal(parseRoute('#/').page, 'overview');
  assert.equal(parseRoute('#/hosts').page, 'hosts');
  assert.equal(parseRoute('#/projects').page, 'projects');
  assert.equal(parseRoute('#/sessions').page, 'sessions');
  assert.equal(parseRoute('#/usage').page, 'usage');
  assert.equal(parseRoute('#/runs').page, 'runs');
  assert.equal(parseRoute('#/workspace').page, 'workspace');
  assert.equal(parseRoute('#/search').page, 'search');
  assert.equal(parseRoute('#/settings').page, 'settings');
  assert.equal(parseRoute('#/devices').page, 'devices');
  assert.equal(parseRoute('#/users').page, 'users');
  assert.equal(parseRoute('#/audit').page, 'audit');
});

test('extracts session and run ids', () => {
  assert.deepEqual(parseRoute('#/sessions/abc'), { page: 'session', id: 'abc' });
  assert.deepEqual(parseRoute('#/sessions/abc/changes'), { page: 'sessionChanges', id: 'abc' });
  assert.deepEqual(parseRoute('#/runs/run-1'), { page: 'run', id: 'run-1' });
});

test('keeps the runs page from matching a run id and settings subpages grouped', () => {
  assert.equal(parseRoute('#/runs').page, 'runs');
  assert.equal(parseRoute('#/settings').page, 'settings');
  assert.equal(parseRoute('').page, 'overview');
});
