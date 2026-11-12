// Visual regression for the synthetic demo. Compares the stable desktop views
// against committed baselines captured at a pinned clock.
//
//   E2E_SPAWN_DEMO=1 npm run e2e:visual   # CI: starts hub + demo
//   DEMO_BASE=http://127.0.0.1:8790 npm run e2e:visual
//   npm run e2e:visual -- --update        # refresh baselines
import { spawn, spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { PNG } from 'pngjs';
import pixelmatch from 'pixelmatch';
import { chromium } from 'playwright';

const here = import.meta.dirname;
const repoRoot = path.resolve(here, '..', '..', '..');
const baselineDir = path.join(here, 'visual-baseline');
const reportDir = path.join(repoRoot, 'visual-report');
const update = process.argv.includes('--update');
const fixedNow = process.env.DEMO_NOW || '2026-11-11T12:00:00Z';
const fixedMs = Date.parse(fixedNow);
const port = Number(process.env.DEMO_PORT || 8790);
const base = process.env.DEMO_BASE || `http://127.0.0.1:${port}`;
const maxRatio = Number(process.env.VISUAL_MAX_RATIO || 0.02);

// Activity views (home, projects) render live elapsed times for the scripted
// demo runs and are intentionally excluded from the pixel diff.
const views = [
  ['#/sessions', 'sessions-dark.png', 'Sessions'],
  ['#/hosts', 'hosts-dark.png', 'Hosts'],
  ['#/settings', 'settings-dark.png', 'Settings'],
];

fs.mkdirSync(baselineDir, { recursive: true });

let demo = null;
async function startDemo() {
  if (process.env.E2E_SPAWN_DEMO !== '1') return;
  const exe = process.platform === 'win32' ? '.exe' : '';
  demo = spawn(process.execPath, [path.join(here, 'seed.mjs')], {
    cwd: repoRoot,
    env: {
      ...process.env,
      DEMO_PORT: String(port),
      NEXUS_DEMO_NOW: fixedNow,
      DEMO_HUB_BIN: path.join(repoRoot, 'bin', `nexus-hub${exe}`),
      DEMO_BIN: path.join(repoRoot, 'bin', `nexus-demo${exe}`),
    },
    stdio: ['ignore', 'pipe', 'pipe'],
    detached: process.platform !== 'win32',
  });
  let output = '';
  const ready = new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`demo did not become ready:\n${output}`)), 90000);
    const onData = (chunk) => {
      output += chunk.toString();
      if (output.includes('READY ')) {
        clearTimeout(timer);
        resolve();
      }
    };
    demo.stdout.on('data', onData);
    demo.stderr.on('data', onData);
    demo.on('exit', (code) => reject(new Error(`demo exited with ${code}:\n${output}`)));
  });
  await ready;
  await new Promise((r) => setTimeout(r, 1500));
}

function stopDemo() {
  if (!demo) return;
  if (process.platform === 'win32') {
    spawnSync('taskkill', ['/PID', String(demo.pid), '/T', '/F'], { stdio: 'ignore' });
  } else {
    try { process.kill(-demo.pid, 'SIGTERM'); } catch { /* already gone */ }
  }
  demo = null;
}

async function freezeClock(context) {
  await context.addInitScript((now) => {
    const NativeDate = Date;
    const offset = now - NativeDate.now();
    class FrozenDate extends NativeDate {
      constructor(...args) {
        if (args.length === 0) super(NativeDate.now() + offset);
        else super(...args);
      }
      static now() { return NativeDate.now() + offset; }
    }
    window.Date = FrozenDate;
  }, fixedMs);
  await context.addInitScript(() => {
    document.addEventListener('DOMContentLoaded', () => {
      const style = document.createElement('style');
      style.textContent = '*,*::before,*::after{animation-duration:0s!important;animation-delay:0s!important;transition:none!important;caret-color:transparent!important}';
      document.head.appendChild(style);
    });
  });
}

async function capture() {
  const browser = await chromium.launch();
  const shots = new Map();
  try {
    const anon = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    await freezeClock(anon);
    await anon.addInitScript(() => localStorage.setItem('nexus-theme', 'dark'));
    const login = await anon.newPage();
    await login.goto(base + '/', { waitUntil: 'domcontentloaded' });
    await login.waitForTimeout(1000);
    shots.set('login-dark.png', await login.screenshot());
    await anon.close();

    const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    await freezeClock(ctx);
    const page = await ctx.newPage();
    await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
    await page.fill('[name=username]', 'demo');
    await page.fill('[name=password]', 'demo-alpha-only');
    await page.click('#login-form button[type=submit]');
    await page.waitForTimeout(1500);
    await page.evaluate(async () => {
      await fetch('/api/v1/me/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-Nexus-Request': '1' },
        body: JSON.stringify({ theme: 'dark' }),
      });
    });
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForSelector('.nav', { timeout: 15000 });
    await page.waitForTimeout(400);
    for (const [route, name, title] of views) {
      await page.goto(base + '/' + route, { waitUntil: 'domcontentloaded' });
      await page.waitForFunction((expected) => document.getElementById('page-title')?.textContent === expected, title, { timeout: 15000 });
      await page.waitForTimeout(700);
      shots.set(name, await page.screenshot());
    }
    await ctx.close();
  } finally {
    await browser.close();
  }
  return shots;
}

function compare(name, current) {
  const baselinePath = path.join(baselineDir, name);
  if (!fs.existsSync(baselinePath)) {
    fs.writeFileSync(baselinePath, current);
    console.log(`baseline created: ${name}`);
    return true;
  }
  const before = PNG.sync.read(fs.readFileSync(baselinePath));
  const after = PNG.sync.read(current);
  if (before.width !== after.width || before.height !== after.height) {
    console.error(`size mismatch: ${name} ${before.width}x${before.height} -> ${after.width}x${after.height}`);
    return false;
  }
  const diff = new PNG({ width: before.width, height: before.height });
  const changed = pixelmatch(before.data, after.data, diff.data, before.width, before.height, { threshold: 0.1 });
  const ratio = changed / (before.width * before.height);
  console.log(`${name}: ${changed} px differ (${(ratio * 100).toFixed(3)}%)`);
  if (ratio <= maxRatio) return true;
  fs.mkdirSync(reportDir, { recursive: true });
  fs.writeFileSync(path.join(reportDir, name), PNG.sync.write(diff));
  fs.writeFileSync(path.join(reportDir, `actual-${name}`), current);
  console.error(`visual regression: ${name} exceeds ${(maxRatio * 100).toFixed(1)}%; see ${reportDir}`);
  return false;
}

let ok = true;
try {
  await startDemo();
  const shots = await capture();
  if (update) {
    fs.mkdirSync(baselineDir, { recursive: true });
    for (const [name, data] of shots) {
      fs.writeFileSync(path.join(baselineDir, name), data);
      console.log(`baseline updated: ${name}`);
    }
  } else {
    for (const [name, data] of shots) {
      if (!compare(name, data)) ok = false;
    }
  }
} catch (err) {
  console.error(err);
  ok = false;
} finally {
  stopDemo();
}
process.exit(ok ? 0 : 1);
