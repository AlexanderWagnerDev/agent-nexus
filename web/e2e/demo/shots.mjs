// Capture README screenshots from the synthetic demo hub. Every value in these
// images comes from cmd/nexus-demo fixtures, never from a real user profile.
import { chromium } from 'playwright';
import { chromiumLaunchOptions } from '../launch.mjs';
import fs from 'node:fs';
import path from 'node:path';

const base = process.env.DEMO_BASE || 'http://127.0.0.1:8790';
const user = process.env.DEMO_USER || 'demo';
const password = process.env.DEMO_PASSWORD || 'demo-alpha-only';
const outDir = path.resolve(process.env.SHOT_DIR || 'docs/screenshots');
fs.mkdirSync(outDir, { recursive: true });

const browser = await chromium.launch(chromiumLaunchOptions());
const errors = [];

async function setTheme(page, theme) {
  await page.evaluate(async (value) => {
    await fetch('/api/v1/me/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'X-Nexus-Request': '1' },
      body: JSON.stringify({ theme: value }),
    });
  }, theme);
  await page.reload({ waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(900);
}

async function login(page) {
  await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
  await page.fill('[name=username]', user);
  await page.fill('[name=password]', password);
  await page.click('#login-form button[type=submit]');
  await page.waitForTimeout(1200);
}

function watch(page) {
  page.on('pageerror', (e) => errors.push(String(e)));
  return page;
}

async function shot(page, name, opts = {}) {
  await page.waitForTimeout(opts.wait ?? 500);
  await page.screenshot({ path: path.join(outDir, name), fullPage: Boolean(opts.full) });
  console.log('shot', name);
}

const api = async (page, pathname) => page.evaluate(async (p) => (await fetch('/api/v1' + p)).json(), pathname);

// Login screens (anonymous, no data).
for (const theme of ['dark', 'light']) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  await ctx.addInitScript((value) => localStorage.setItem('nexus-theme', value), theme);
  const page = watch(await ctx.newPage());
  await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(900);
  await shot(page, `login-${theme}.png`);
  await ctx.close();
}

for (const theme of ['dark', 'light']) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  await ctx.addInitScript((value) => localStorage.setItem('nexus-theme', value), theme);
  const page = watch(await ctx.newPage());
  await login(page);
  await page.goto(base + '/#/', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(600);
  await setTheme(page, theme);

  for (const [route, name] of [['#/', 'dashboard'], ['#/projects', 'projects'], ['#/hosts', 'hosts'], ['#/settings', 'settings']]) {
    await page.goto(base + '/' + route, { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(900);
    await shot(page, `${name}-${theme}.png`);
  }

  await page.goto(base + '/#/sessions', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(900);
  await shot(page, `sessions-${theme}.png`);

  await page.goto(base + '/#/runs', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(900);
  await shot(page, theme === 'dark' ? 'multi-agent-dashboard.png' : 'runs-light.png');

  const { runs } = await api(page, '/runs');
  const chatRun = runs.find((r) => /upload retries/i.test(r.session_title)) || runs[0];
  const sessions = (await api(page, '/sessions?limit=50')).sessions;
  const demoSession = sessions.find((s) => /upload retries/i.test(s.title)) || sessions[0];

  if (chatRun) {
    await page.goto(`${base}/#/runs/${chatRun.id}`, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('.turn-assistant', { timeout: 15000 });
    await page.waitForTimeout(700);
    await shot(page, `chat-${theme}.png`);
    const waitingRun = runs.find((run) => run.state === 'waiting_for_input');
    if (theme === 'dark' && waitingRun) {
      await page.goto(`${base}/#/runs/${waitingRun.id}`, { waitUntil: 'domcontentloaded' });
      await page.waitForSelector('.approval-card', { timeout: 15000 });
      await page.waitForTimeout(500);
      await shot(page, 'chat-approval-dark.png');
      await page.goto(`${base}/#/runs/${chatRun.id}`, { waitUntil: 'domcontentloaded' });
      await page.waitForSelector('.turn-assistant', { timeout: 15000 });
      await page.waitForTimeout(500);
    }
    if (theme === 'dark') {
      const tool = page.locator('.tool-card').first();
      if (await tool.count()) {
        await tool.scrollIntoViewIfNeeded();
        await tool.click();
        await page.waitForTimeout(400);
        await shot(page, 'chat-tools-dark.png');
      }
      const edit = page.locator('.tool-card', { hasText: 'Edited file' }).first();
      if (await edit.count()) {
        await edit.click();
        await page.waitForTimeout(500);
        const diff = page.locator('.tool-card .diff-panel').first();
        if (await diff.count()) {
          await diff.scrollIntoViewIfNeeded();
          await shot(page, 'chat-diff-dark.png');
          await edit.click();
        }
      }
      const contextToggle = page.locator('#chat-context-toggle');
      if (await contextToggle.isVisible()) await contextToggle.click();
      await page.waitForTimeout(500);
      await shot(page, 'chat-context-dark.png');
      const composer = page.locator('#prompt-text');
      if (await composer.count()) {
        await composer.scrollIntoViewIfNeeded();
        await composer.fill('Also add a note to the changelog, please.');
        await shot(page, 'chat-composer-dark.png');
        await composer.fill('');
      }
      const model = page.locator('#opt-model-btn');
      if (await model.count()) {
        await model.click();
        await page.waitForTimeout(500);
        await shot(page, 'model-picker-dark.png');
        await page.keyboard.press('Escape');
      }
    }
  }

  if (demoSession) {
    await page.goto(`${base}/#/sessions/${demoSession.id}`, { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(1400);
    await shot(page, `session-detail-${theme === 'dark' ? 'dark' : 'light'}.png`);
    await page.goto(`${base}/#/sessions/${demoSession.id}/changes`, { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(1400);
    await page.locator('[data-diff-mode="split"]').first().click();
    await page.waitForTimeout(500);
    await shot(page, `diff-split-${theme}.png`);
  }

  if (runs.length >= 2) {
    await page.goto(`${base}/#/workspace?pane=${runs[0].id}&pane=${runs[1].id}`, { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(1400);
    await shot(page, `workspace-${theme}.png`);
  }

  await page.goto(base + '/#/', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(700);
  if (theme === 'dark') {
    await page.locator('#notif-toggle').click();
    await page.waitForTimeout(600);
    await shot(page, 'notifications-dark.png');
    await page.locator('#notif-toggle').click();
  }
  await page.keyboard.press('Control+K');
  await page.waitForTimeout(500);
  await shot(page, 'command-palette.png');
  await page.keyboard.press('Escape');

  await ctx.close();
}

// Mobile views.
for (const theme of ['dark', 'light']) {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  await ctx.addInitScript((value) => localStorage.setItem('nexus-theme', value), theme);
  const page = watch(await ctx.newPage());
  await login(page);
  await page.goto(base + '/#/', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(600);
  await setTheme(page, theme);
  await shot(page, theme === 'dark' ? 'mobile-dashboard.png' : 'mobile-dashboard-light.png');
  const { runs } = await api(page, '/runs');
  if (theme === 'dark') {
    for (const [route, name] of [['#/projects', 'mobile-projects'], ['#/sessions', 'mobile-sessions'], ['#/runs', 'mobile-runs'], ['#/hosts', 'mobile-hosts'], ['#/settings', 'mobile-settings']]) {
      await page.goto(base + '/' + route, { waitUntil: 'domcontentloaded' });
      await page.waitForTimeout(800);
      await shot(page, `${name}.png`);
    }
    if (runs.length >= 2) {
      await page.goto(`${base}/#/workspace?pane=${runs[0].id}&pane=${runs[1].id}`, { waitUntil: 'domcontentloaded' });
      await page.waitForTimeout(1200);
      await shot(page, 'workspace-mobile.png');
    }
  }
  const chatRun = runs.find((r) => /upload retries/i.test(r.session_title)) || runs[0];
  if (chatRun) {
    await page.goto(`${base}/#/runs/${chatRun.id}`, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('.turn-assistant', { timeout: 15000 });
    await page.waitForTimeout(700);
    await shot(page, theme === 'dark' ? 'mobile-chat.png' : 'mobile-chat-light.png');
    const details = page.locator('#chat-context-toggle');
    if (await details.isVisible()) {
      await details.click();
      await page.waitForTimeout(500);
      await shot(page, theme === 'dark' ? 'mobile-chat-details.png' : 'mobile-chat-details-light.png');
    }
  }
  await ctx.close();
}

await browser.close();
console.log(errors.length ? `page errors: ${errors.join(' | ')}` : 'no page errors');
