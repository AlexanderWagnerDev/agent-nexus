import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const skipDirs = new Set(['.git', 'node_modules', 'bin', 'dist']);
const skipExt = new Set(['.png', '.jpg', '.jpeg', '.gif', '.ico', '.zip', '.gz', '.exe', '.woff', '.woff2']);

const checks = [
  ['personal email domain', /@alexanderwagnerdev\.com/i],
  ['consumer email address', /@(gmail|outlook|hotmail|yahoo)\./i],
  ['non-demo Windows profile path', /[A-Za-z]:\\Users\\(?!demo\\|me\\|Public\\)/i],
  ['non-demo home path', /\/(?:home|Users)\/(?!demo\/|demo\b|me\/|me\b|user\/|user\b)/],
  ['developer machine path', /\bQ:\/(?:GitHub|tmp)/i],
  ['private key block', /BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY/],
  ['GitHub token', /\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})\b/],
  ['OpenAI-style key', /\bsk-[A-Za-z0-9]{24,}\b/],
];

function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name.startsWith('.') && entry.name !== '.github' && entry.name !== '.env.example') {
      if (entry.isDirectory()) continue;
    }
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!skipDirs.has(entry.name)) walk(full, out);
    } else if (!skipExt.has(path.extname(entry.name).toLowerCase()) && fs.statSync(full).size < 1_000_000) {
      out.push(full);
    }
  }
  return out;
}

test('publishable text files contain no personal identity or secrets', () => {
  const findings = [];
  const self = path.join(root, 'web', 'test', 'privacy.test.mjs');
  for (const file of walk(root)) {
    if (file === self) continue;
    let text;
    try {
      text = fs.readFileSync(file, 'utf8');
    } catch {
      continue;
    }
    for (const [label, pattern] of checks) {
      const match = pattern.exec(text);
      if (match) {
        const line = text.slice(0, match.index).split('\n').length;
        findings.push(`${label}: ${path.relative(root, file)}:${line}`);
      }
    }
  }
  assert.deepEqual(findings, []);
});
