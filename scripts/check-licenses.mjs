#!/usr/bin/env node
/*
 * License gate for CI. It checks Go modules and every package in the browser
 * lockfile against the project's allowed set. MPL-2.0 is accepted only for
 * explicitly listed packages so a new copyleft dependency cannot slip in
 * silently.
 */
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const allowed = new Set(['MIT', 'BSD-2-Clause', 'BSD-3-Clause', 'Apache-2.0', 'ISC', '0BSD']);
const exceptionsPath = path.join(root, 'scripts', 'license-exceptions.json');
const exceptions = fs.existsSync(exceptionsPath) ? JSON.parse(fs.readFileSync(exceptionsPath, 'utf8')) : {};
const failures = [];

function detect(text) {
  const lower = text.toLowerCase();
  if (lower.includes('mozilla public license') && lower.includes('version 2.0')) return 'MPL-2.0';
  if (lower.includes('apache license') && lower.includes('version 2.0')) return 'Apache-2.0';
  if (lower.includes('permission to use, copy, modify, and/or distribute this software for any purpose')) return 'ISC';
  if (lower.includes('permission to use, copy, modify') && lower.includes('with or without fee')) return 'ISC';
  if (lower.includes('permission is hereby granted, free of charge')) return 'MIT';
  if (lower.includes('redistribution and use in source and binary forms')) {
    return /3\.\s*Neither the name/i.test(text) ? 'BSD-3-Clause' : 'BSD-2-Clause';
  }
  if (lower.includes('boost software license')) return 'BSL-1.0';
  return 'UNKNOWN';
}

function licenseFile(dir) {
  let entries;
  try { entries = fs.readdirSync(dir); } catch { return null; }
  const match = entries.find((name) => /^(license|licence|copying)(\.|$)/i.test(name));
  return match ? path.join(dir, match) : null;
}

function checkGo() {
  let raw;
  try {
    raw = execFileSync('go', ['list', '-m', '-json', 'all'], { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
  } catch (error) {
    failures.push(`go list failed: ${error.message}`);
    return;
  }
  const modules = [];
  let depth = 0;
  let start = -1;
  for (let i = 0; i < raw.length; i++) {
    if (raw[i] === '{') { if (depth === 0) start = i; depth++; }
    else if (raw[i] === '}') {
      depth--;
      if (depth === 0 && start >= 0) {
        modules.push(JSON.parse(raw.slice(start, i + 1)));
        start = -1;
      }
    }
  }
  const mainModule = modules.find((m) => m.Main);
  for (const mod of modules) {
    if (mod.Main || mod.Path === mainModule?.Path) continue;
    if (!mod.Dir) continue;
    const file = licenseFile(mod.Dir);
    const spdx = file ? detect(fs.readFileSync(file, 'utf8')) : 'UNKNOWN';
    if (allowed.has(spdx)) continue;
    const exception = exceptions[spdx] || [];
    if (exception.includes(mod.Path)) continue;
    failures.push(`Go module ${mod.Path}@${mod.Version}: ${spdx} (no documented exception)`);
  }
  console.log(`checked ${modules.length - 1} Go modules`);
}

function checkNpm() {
  const lockPath = path.join(root, 'web', 'package-lock.json');
  if (!fs.existsSync(lockPath)) { failures.push('web/package-lock.json is missing'); return; }
  const lock = JSON.parse(fs.readFileSync(lockPath, 'utf8'));
  let count = 0;
  for (const [key, pkg] of Object.entries(lock.packages || {})) {
    if (!key.startsWith('node_modules/')) continue;
    const name = key.replace(/^node_modules\//, '');
    let license = pkg.license;
    if (!license || typeof license !== 'string') {
      try {
        const installed = JSON.parse(fs.readFileSync(path.join(root, 'web', key, 'package.json'), 'utf8'));
        license = installed.license;
      } catch { /* handled below */ }
    }
    if (typeof license !== 'string') license = 'UNKNOWN';
    // Some packages use SPDX expressions such as "(MIT OR Apache-2.0)".
    const choices = license.replace(/[()]/g, '').split(/\s+OR\s+|\s+AND\s+/).map((v) => v.trim());
    if (choices.every((choice) => allowed.has(choice))) { count++; continue; }
    const exception = exceptions[license] || [];
    if (exception.includes(name)) { count++; continue; }
    failures.push(`npm package ${name}@${pkg.version || ''}: ${license} (no documented exception)`);
  }
  console.log(`checked ${count} npm packages`);
}

checkGo();
checkNpm();
if (failures.length) {
  console.error('\nLicense policy violations:');
  for (const failure of failures) console.error(`  - ${failure}`);
  console.error('\nAllowed: MIT, BSD-2-Clause, BSD-3-Clause, Apache-2.0, ISC, 0BSD.');
  console.error('MPL-2.0 is accepted only for packages listed in scripts/license-exceptions.json.');
  process.exit(1);
}
console.log('license policy passed');
