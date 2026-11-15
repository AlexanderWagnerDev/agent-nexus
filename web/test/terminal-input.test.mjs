import assert from 'node:assert/strict';
import test from 'node:test';
import { b64decode, b64encode } from '../assets/ui.js';

test('encodes terminal input larger than the spread-argument limit', () => {
  // btoa(String.fromCharCode(...bytes)) throws above roughly 125k bytes, which
  // silently dropped every large paste. The chunked encoder must cope with the
  // 2 MiB the hub accepts.
  const bytes = new Uint8Array(300000);
  for (let i = 0; i < bytes.length; i += 1) bytes[i] = i % 251;
  const encoded = b64encode(bytes);
  assert.equal(encoded.length, Math.ceil(bytes.length / 3) * 4);
  const decoded = b64decode(encoded);
  assert.deepEqual(Array.from(decoded), Array.from(bytes));
});

test('encodes a short ascii payload the same way as btoa', () => {
  const bytes = new TextEncoder().encode('hello terminal');
  assert.equal(b64encode(bytes), btoa('hello terminal'));
});

test('the spread form fails above the argument limit, which is why it is not used', () => {
  const bytes = new Uint8Array(300000);
  assert.throws(() => btoa(String.fromCharCode(...bytes)), RangeError);
});
