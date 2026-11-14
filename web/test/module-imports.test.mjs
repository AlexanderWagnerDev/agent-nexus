import test from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

// Regression guard for the "page renders a ReferenceError" class: a module
// that uses a shared helper must import it. The three broken pages (search,
// audit, settings) all used a symbol without importing it.
const assets = fileURLToPath(new URL('../assets/', import.meta.url));

// Symbols whose absence is a real user-visible crash. Deliberately small and
// unambiguous: names that are also used as local parameters are not listed.
const required = {
  state: '../state.js',
  api: '../api.js',
  fmtStamp: '../ui-format.js',
  fmtTime: '../ui-format.js',
  fmtRelative: '../ui-format.js',
  countLabel: '../ui-format.js',
  openTab: '../tabs.js',
  storeGet: '../state.js',
  storeSet: '../state.js',
};

function walk(dir, files = []) {
  for (const name of readdirSync(dir).sort()) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      if (name === 'vendor') continue;
      walk(path, files);
    } else if (name.endsWith('.js')) {
      files.push(path);
    }
  }
  return files;
}

function importedNames(source) {
  const names = new Set();
  for (const match of source.matchAll(/import\s*\{([^}]+)\}\s*from\s*['"][^'"]+['"]/g)) {
    for (const part of match[1].split(',')) {
      const name = part.trim().split(/\s+as\s+/)[0].trim();
      if (name) names.add(name);
    }
  }
  return names;
}

function declaredNames(source) {
  const names = new Set();
  for (const match of source.matchAll(/(?:^|\n)\s*(?:export\s+)?(?:async\s+)?(?:function\*?|const|let|var|class)\s+([A-Za-z_$][\w$]*)/g)) {
    names.add(match[1]);
  }
  return names;
}

test('every module imports the shared helpers it uses', () => {
  for (const file of walk(assets)) {
    const source = readFileSync(file, 'utf8');
    const imported = importedNames(source);
    const declared = declaredNames(source);
    const body = source
      .replace(/^import[\s\S]*?;$/gm, '')
      .replace(/\/\*[\s\S]*?\*\//g, '')
      .replace(/'(?:\\.|[^'\\])*'/g, "''")
      .replace(/"(?:\\.|[^"\\])*"/g, '""');
    for (const [symbol, origin] of Object.entries(required)) {
      if (declared.has(symbol)) continue;
      const used = new RegExp(`(^|[^\\w.$])${symbol}\\s*[.(]`, 'm').test(body);
      if (!used || imported.has(symbol)) continue;
      assert.fail(`${file.slice(assets.length)} uses "${symbol}" without importing it (from ${origin})`);
    }
  }
});
