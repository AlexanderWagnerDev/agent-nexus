import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');

function sourceFiles(dir) {
  const out = [];
  for (const entry of fs.readdirSync(path.join(root, dir), { withFileTypes: true })) {
    const relative = path.posix.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...sourceFiles(relative));
    else if (entry.name.endsWith('.js')) out.push(relative);
  }
  return out;
}

const sources = [
  'web/assets/app.js',
  'web/assets/chat-format.js',
  'web/assets/markdown.js',
  ...sourceFiles('web/assets/views'),
  ...sourceFiles('web/assets/components'),
];

function unescapeKey(value) {
  return value.replace(/\\'/g, "'").replace(/\\"/g, '"').replace(/\\\\/g, '\\');
}

function usedKeys() {
  const keys = new Set();
  for (const file of sources) {
    const text = fs.readFileSync(path.join(root, file), 'utf8');
    for (const match of text.matchAll(/\bt\('((?:[^'\\]|\\.)*)'/g)) keys.add(unescapeKey(match[1]));
    for (const match of text.matchAll(/\bt\("((?:[^"\\]|\\.)*)"/g)) keys.add(unescapeKey(match[1]));
  }
  return keys;
}

function definedKeys() {
  const text = fs.readFileSync(path.join(root, 'web/assets/i18n.js'), 'utf8');
  const keys = new Set();
  const re = /^\s*(?:'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)"|([A-Za-z_$][\w$]*))\s*:/gm;
  for (const match of text.matchAll(re)) keys.add(unescapeKey(match[1] ?? match[2] ?? match[3]));
  return keys;
}

test('every literal t() key has a dictionary entry', () => {
  const known = definedKeys();
  const missing = [...usedKeys()].filter((key) => !known.has(key));
  assert.deepEqual(missing, [], `missing translations: ${missing.join(', ')}`);
});

test('dictionary has no empty values', () => {
  const text = fs.readFileSync(path.join(root, 'web/assets/i18n.js'), 'utf8');
  const empty = [...text.matchAll(/^\s*(?:'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)")\s*:\s*''\s*,?$/gm)]
    .map((match) => match[1] ?? match[2]);
  assert.deepEqual(empty, [], `empty translations: ${empty.join(', ')}`);
});
