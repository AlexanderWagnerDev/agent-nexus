import assert from 'node:assert/strict';
import test from 'node:test';
import { hasSavedAccountArtifacts, shouldClearAccountArtifacts } from '../assets/state.js';

// localStorage is not available in plain Node; the helpers only use the
// standard Storage methods and are written to fail closed without it. The
// proxy exposes stored keys as enumerable own properties like a real Storage.
function installStorage(entries = {}) {
  const data = new Map(Object.entries(entries).map(([key, value]) => [key, String(value)]));
  const storage = {
    getItem: (key) => (data.has(key) ? data.get(key) : null),
    setItem: (key, value) => { data.set(key, String(value)); },
    removeItem: (key) => { data.delete(key); },
    key: (index) => Array.from(data.keys())[index] ?? null,
    get length() { return data.size; },
  };
  globalThis.localStorage = new Proxy(storage, {
    ownKeys: () => Array.from(data.keys()),
    getOwnPropertyDescriptor: (target, prop) => (data.has(prop)
      ? { enumerable: true, configurable: true, value: data.get(prop) }
      : Object.getOwnPropertyDescriptor(target, prop)),
    get: (target, prop) => (data.has(prop) ? data.get(prop) : target[prop]),
  });
}

test('unowned recent-session hints alone require a clear', () => {
  installStorage({ 'nexus-recent': '[{"id":"ses-1"}]' });
  assert.equal(hasSavedAccountArtifacts(), true);
  assert.equal(shouldClearAccountArtifacts('', 'user-b'), true);
});

test('every saved artifact kind is detected', () => {
  for (const key of ['nexus-tabs', 'nexus-recent', 'nexus-draft-run-1', 'nexus-run-config-run-1']) {
    installStorage({ [key]: '[]' });
    assert.equal(hasSavedAccountArtifacts(), true, key);
    assert.equal(shouldClearAccountArtifacts('', 'user-b'), true, key);
  }
});

test('an empty store needs no clear', () => {
  installStorage();
  assert.equal(hasSavedAccountArtifacts(), false);
  assert.equal(shouldClearAccountArtifacts('', 'user-b'), false);
});

test('artifacts owned by the signing-in account are kept', () => {
  installStorage({ 'nexus-recent': '[]' });
  assert.equal(shouldClearAccountArtifacts('user-a', 'user-a'), false);
  assert.equal(shouldClearAccountArtifacts('user-a', 'user-b'), true);
  assert.equal(shouldClearAccountArtifacts('', ''), false);
});
