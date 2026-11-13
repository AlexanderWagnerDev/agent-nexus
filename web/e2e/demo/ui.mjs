// UI regression checks against the synthetic demo hub. Covers the behaviours
// that need a live DOM: tab dedupe, cross-host project grouping, the bell badge,
// host overflow actions, the Git diffstat list and mobile navigation.
//
//   E2E_SPAWN_DEMO=1 node web/e2e/demo/ui.mjs
//   DEMO_BASE=http://127.0.0.1:8790 node web/e2e/demo/ui.mjs
import { spawn, spawnSync } from 'node:child_process';
import path from 'node:path';
import { chromium } from 'playwright';
import { chromiumLaunchOptions } from '../launch.mjs';

const here = import.meta.dirname;
const repoRoot = path.resolve(here, '..', '..', '..');
const port = Number(process.env.DEMO_PORT || 8790);
const base = process.env.DEMO_BASE || `http://127.0.0.1:${port}`;
const user = process.env.DEMO_USER || 'demo';
const password = process.env.DEMO_PASSWORD || 'demo-alpha-only';

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

let demo = null;
async function startDemo() {
  if (process.env.E2E_SPAWN_DEMO !== '1') return;
  const exe = process.platform === 'win32' ? '.exe' : '';
  demo = spawn(process.execPath, [path.join(here, 'seed.mjs')], {
    cwd: repoRoot,
    env: {
      ...process.env,
      DEMO_PORT: String(port),
      DEMO_HUB_BIN: path.join(repoRoot, 'bin', `nexus-hub${exe}`),
      DEMO_BIN: path.join(repoRoot, 'bin', `nexus-demo${exe}`),
    },
    stdio: ['ignore', 'pipe', 'pipe'],
    detached: process.platform !== 'win32',
  });
  let output = '';
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`demo did not become ready:\n${output}`)), 90000);
    const onData = (chunk) => {
      output += chunk.toString();
      if (output.includes('READY ')) { clearTimeout(timer); resolve(); }
    };
    demo.stdout.on('data', onData);
    demo.stderr.on('data', onData);
    demo.on('exit', (code) => reject(new Error(`demo exited with ${code}:\n${output}`)));
  });
  await new Promise((resolve) => setTimeout(resolve, 1500));
}
function stopDemo() {
  if (!demo) return;
  if (process.platform === 'win32') spawnSync('taskkill', ['/PID', String(demo.pid), '/T', '/F'], { stdio: 'ignore' });
  else { try { process.kill(-demo.pid, 'SIGTERM'); } catch { /* gone */ } }
  demo = null;
}

async function login(page) {
  await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
  await page.fill('[name=username]', user);
  await page.fill('[name=password]', password);
  await page.click('#login-form button[type=submit]');
  await page.waitForTimeout(1400);
}

let ok = true;
try {
  await startDemo();
  const browser = await chromium.launch(chromiumLaunchOptions());
  try {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const page = await ctx.newPage();
    const errors = [];
    page.on('pageerror', (error) => errors.push(String(error)));
    await login(page);

    await page.goto(base + '/#/', { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(900);
    const homeNames = await page.locator('.project-list .project-row-main strong').allInnerTexts();
    assert(homeNames.filter((name) => name === 'example-service').length === 1, 'example-service is not grouped on Home');
    assert(homeNames.filter((name) => name === 'sample-api').length === 1, 'sample-api is not grouped on Home');
    const multiHost = await page.locator('.project-row-main small', { hasText: '2 hosts' }).count();
    assert(multiHost >= 2, 'Home does not label repositories shared by two hosts');

    const api = async (pathname) => page.evaluate(async (p) => (await (await fetch('/api/v1' + p)).json()), pathname);
    const { runs } = await api('/runs');
    const upload = runs.find((run) => /upload retries/i.test(run.session_title));
    const pagination = runs.find((run) => /pagination/i.test(run.session_title));
    assert(upload && pagination, 'demo runs missing');
    if (upload.session_id) {
      await page.goto(`${base}/#/runs/${upload.id}`, { waitUntil: 'domcontentloaded' });
      await page.waitForTimeout(1200);
      await page.goto(`${base}/#/sessions/${upload.session_id}`, { waitUntil: 'domcontentloaded' });
      await page.waitForTimeout(1200);
      const sameTitleTabs = await page.locator('.tab', { hasText: 'Refactor upload retries' }).count();
      assert(sameTitleTabs === 1, `duplicate session tabs: ${sameTitleTabs}`);
    }

    await page.goto(`${base}/#/workspace?pane=${upload.id}&pane=${pagination.id}`, { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(1200);
    assert((await page.locator('.nav a.active').getAttribute('href')) === '#/workspace', 'workspace does not have its own active nav state');
    assert((await page.locator('.split-pane').count()) === 2, 'split workspace did not render two panes');

    const bell = await page.evaluate(() => {
      const icon = document.querySelector('#notif-toggle svg').getBoundingClientRect();
      const badge = document.getElementById('notif-badge');
      badge.textContent = '9';
      badge.classList.add('visible');
      const dot = badge.getBoundingClientRect();
      const cx = icon.left + icon.width / 2;
      const cy = icon.top + icon.height / 2;
      return { covered: cx >= dot.left && cx <= dot.right && cy >= dot.top && cy <= dot.bottom, width: icon.width };
    });
    assert(bell.width >= 18 && !bell.covered, 'notification badge covers the bell icon');

    await page.goto(base + '/#/hosts', { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(900);
    const rows = page.locator('.host-row');
    assert((await rows.count()) === 3, 'expected three demo hosts');
    const directActions = await rows.first().locator('.host-actions > .btn, .host-actions > .icon-btn').count();
    assert(directActions <= 3, `too many direct host actions: ${directActions}`);
    await rows.first().locator('.action-menu summary').click();
    assert(await rows.first().locator('.action-menu-panel [data-revoke-host]').isVisible(), 'revoke is not in the host overflow menu');
    await page.keyboard.press('Escape');

    await page.goto(`${base}/#/runs/${pagination.id}`, { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(1200);
    const config = await page.locator('#composer-config').innerText();
    assert(/GPT-5\.3 Codex/.test(config), `Codex run does not show an OpenAI model: ${config}`);
    await page.click('#chat-context-toggle');
    await page.waitForTimeout(1000);
    assert((await page.locator('.git-stat-list li').count()) >= 2, 'git diffstat is not rendered as a list');
    const gitHead = await page.locator('.git-context-head').innerText();
    assert(!/2026/.test(gitHead), `git timestamp is not compact: ${gitHead}`);
    await ctx.close();

    const mobile = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
    const phone = await mobile.newPage();
    await login(phone);
    await phone.goto(`${base}/#/workspace?pane=${upload.id}&pane=${pagination.id}`, { waitUntil: 'domcontentloaded' });
    await phone.waitForTimeout(1200);
    const navCount = await phone.locator('.nav a:visible, .nav-more:visible').count();
    assert(navCount === 6, `expected five primary entries plus More, got ${navCount}`);
    await phone.click('#mobile-more');
    assert(await phone.locator('.mobile-more-list a[href="#/settings"]').isVisible(), 'More menu misses settings');
    await phone.keyboard.press('Escape');
    const topbar = await phone.evaluate(() => {
      const style = getComputedStyle(document.querySelector('.topbar'));
      return { position: style.position, background: style.backgroundColor };
    });
    assert(topbar.position === 'sticky', 'mobile header is not sticky');
    const alpha = /rgba\([^)]*,\s*([0-9.]+)\)/.exec(topbar.background);
    assert(!alpha || Number(alpha[1]) === 1, `mobile header is see-through: ${topbar.background}`);
    await mobile.close();

    assert(!errors.length, `page errors: ${errors.join(' | ')}`);
    console.log('demo UI regression checks passed');
  } finally {
    await browser.close();
  }
} catch (error) {
  console.error(error);
  ok = false;
} finally {
  stopDemo();
}
process.exit(ok ? 0 : 1);
