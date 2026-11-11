import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const readme = fs.readFileSync(path.join(root, 'README.md'), 'utf8');
const refs = [...new Set([...readme.matchAll(/docs\/screenshots\/[A-Za-z0-9._-]+/g)].map((m) => m[0]))];

function pngSize(buffer) {
  if (buffer.length < 24 || buffer.toString('ascii', 1, 4) !== 'PNG') return null;
  return { width: buffer.readUInt32BE(16), height: buffer.readUInt32BE(20) };
}

test('every screenshot referenced by the README exists and is a real PNG', () => {
  assert.ok(refs.length >= 20, `expected at least 20 screenshot references, got ${refs.length}`);
  for (const ref of refs) {
    const file = path.join(root, ref);
    assert.ok(fs.existsSync(file), `missing ${ref}`);
    const stat = fs.statSync(file);
    assert.ok(stat.size > 5000, `${ref} looks empty (${stat.size} bytes)`);
    const size = pngSize(fs.readFileSync(file));
    assert.ok(size, `${ref} is not a PNG`);
    assert.ok(size.width >= 300 && size.height >= 300, `${ref} is too small: ${size.width}x${size.height}`);
  }
});

test('screenshot references are unique and tracked', () => {
  assert.equal(refs.length, new Set(refs).size);
});
