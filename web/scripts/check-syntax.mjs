import { execFileSync } from 'node:child_process';
import { readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

// Check every first-party module, including views/ and components/. A missing
// semicolon or bracket in a nested view used to escape this check entirely.
const assets = fileURLToPath(new URL('../assets/', import.meta.url));

function walk(dir) {
  for (const name of readdirSync(dir).sort()) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      if (name === 'vendor') continue;
      walk(path);
      continue;
    }
    if (!name.endsWith('.js')) continue;
    execFileSync(process.execPath, ['--check', path], { stdio: 'inherit' });
  }
}

walk(assets);
console.log('JavaScript syntax check passed');
