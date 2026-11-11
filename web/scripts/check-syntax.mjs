import { execFileSync } from 'node:child_process';
import { readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const assets = new URL('../assets/', import.meta.url);
for (const name of readdirSync(assets).sort()) {
  if (!name.endsWith('.js')) continue;
  const file = fileURLToPath(new URL(name, assets));
  execFileSync(process.execPath, ['--check', file], { stdio: 'inherit' });
}
console.log('JavaScript syntax check passed');
