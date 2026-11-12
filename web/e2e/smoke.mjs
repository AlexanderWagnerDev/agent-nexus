// Browser smoke test for the embedded AgentNexus web client.
//
// It can start a disposable hub itself (E2E_SPAWN_HUB=1) or target an existing
// one via BASE_URL. It verifies login, navigation, themes, persistence, and the
// responsive layout. Run it with `npm run e2e` from this directory.
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '../..');
let child = null;
let baseURL = process.env.BASE_URL || 'http://localhost:18787';
const username = process.env.E2E_USER || 'owner';
const password = process.env.E2E_PASSWORD || 'correct-horse-battery';

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

async function waitFor(url, timeoutMs = 20000) {
  const until = Date.now() + timeoutMs;
  while (Date.now() < until) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch { /* not ready */ }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`timed out waiting for ${url}`);
}

async function startHub() {
  if (process.env.E2E_SPAWN_HUB !== '1') return;
  const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'agentnexus-e2e-'));
  const bin = process.env.NEXUS_HUB_BIN || path.join(root, 'bin', process.platform === 'win32' ? 'nexus-hub.exe' : 'nexus-hub');
  const port = new URL(baseURL).port || '18787';
  child = spawn(bin, ['--addr', `127.0.0.1:${port}`, '--data-dir', dataDir,
    '--public-url', `http://localhost:${port}`, '--log-level', 'error'], { stdio: 'inherit' });
  await waitFor(`${baseURL}/healthz`);
}

async function ensureSetup() {
  const status = await (await fetch(`${baseURL}/api/v1/setup/status`)).json();
  if (!status.setup_required) return;
  const response = await fetch(`${baseURL}/api/v1/setup`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Nexus-Request': '1' },
    body: JSON.stringify({ username, password }),
  });
  assert(response.ok, `setup failed: ${response.status} ${await response.text()}`);
}

async function login(page) {
  await page.goto(`${baseURL}/`, { waitUntil: 'networkidle' });
  if (await page.locator('#login-form').count()) {
    await page.fill('[name=username]', username);
    await page.fill('[name=password]', password);
    await page.click('#login-form button[type=submit]');
    await page.waitForTimeout(1200);
  }
  assert(await page.locator('.nav a[href="#/sessions"]').count(), 'navigation did not render after login');
}

async function themeOf(page) {
  return page.evaluate(() => ({
    resolved: document.documentElement.dataset.theme,
    mode: document.documentElement.dataset.themeMode,
  }));
}

async function run() {
  await startHub();
  await ensureSetup();
  const browser = await chromium.launch(process.env.E2E_CHROME ? { executablePath: process.env.E2E_CHROME } : {});
  try {
    const desktop = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const page = await desktop.newPage();
    await login(page);

    await page.goto(`${baseURL}/#/settings`, { waitUntil: 'networkidle' });
    await page.waitForSelector('[data-theme-choice=dark]');
    await page.click('[data-theme-choice=dark]');
    await page.waitForFunction(() => document.documentElement.dataset.theme === 'dark');
    assert((await themeOf(page)).mode === 'dark', 'dark mode was not selected');
    await page.reload({ waitUntil: 'networkidle' });
    assert((await themeOf(page)).resolved === 'dark', 'dark theme did not survive a reload');
    assert(await page.locator('[data-theme-choice=dark].active').count(), 'settings did not show the stored theme');

    await page.click('[data-theme-choice=light]');
    await page.waitForFunction(() => document.documentElement.dataset.theme === 'light');
    await page.reload({ waitUntil: 'networkidle' });
    assert((await themeOf(page)).resolved === 'light', 'light theme did not survive a reload');

    await page.click('[data-theme-choice=system]');
    await page.waitForFunction(() => document.documentElement.dataset.themeMode === 'system');
    assert((await themeOf(page)).mode === 'system', 'system mode was not stored');

    for (const route of ['#/', '#/projects', '#/hosts', '#/sessions', '#/runs', '#/workspace', '#/search', '#/settings']) {
      await page.goto(`${baseURL}/${route}`, { waitUntil: 'networkidle' });
      assert(await page.locator('#view').count(), `view missing for ${route}`);
    }

    await page.goto(`${baseURL}/#/`, { waitUntil: 'networkidle' });
    await page.keyboard.press('Control+K');
    await page.waitForSelector('#palette-input');
    assert((await page.locator('.palette-row').count()) > 0, 'command palette has no actions');
    await page.fill('#palette-input', 'settings');
    await page.waitForTimeout(500);
    assert((await page.locator('.palette-row').count()) > 0, 'command palette search returned nothing');
    await page.keyboard.press('Escape');
    assert(await page.locator('#palette-input').count() === 0, 'command palette did not close');

    await page.click('#sidebar-toggle');
    assert(await page.locator('#layout.sidebar-collapsed').count() === 1, 'sidebar did not collapse');
    await page.click('#sidebar-toggle');

    assert(await page.locator('#notif-toggle').count() === 1, 'notification button missing');
    await page.click('#notif-toggle');
    await page.waitForSelector('#notif-panel:not([hidden])');
    await page.keyboard.press('Escape');

    // The conflict dialog is reproduced without a real host: a synthetic
    // session reports active_elsewhere and the run endpoint answers 409.
    const conflict = await desktop.newPage();
    const session = {
      id: 'fake-session', host_id: 'fake-host', host_name: 'synthetic', agent: 'opencode',
      native_id: 'native-1', title: 'Conflict fixture', cwd: '/tmp/conflict', message_count: 3,
      state: 'idle', active_elsewhere: true, tags: ['demo'],
      created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    };
    await conflict.route('**/api/v1/**', async (route) => {
      const request = route.request();
      const pathname = new URL(request.url()).pathname;
      const json = (status, body) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
      if (pathname === '/api/v1/hosts') return json(200, { hosts: [{ id: 'fake-host', name: 'synthetic', status: 'online', agents: [{ name: 'opencode', capabilities: ['structured'], version: '2.2.0' }] }] });
      if (pathname === '/api/v1/sessions' && request.method() === 'GET') return json(200, { sessions: [session], has_more: false, offset: 0 });
      if (pathname === '/api/v1/sessions/fake-session' && request.method() === 'GET') return json(200, session);
      if (pathname === '/api/v1/sessions/fake-session/transcript') return json(200, { messages: [], total: 0 });
      if (pathname === '/api/v1/sessions/fake-session/runs') return json(409, { error: 'session is open in a native terminal', reason: 'active_elsewhere' });
      if (pathname === '/api/v1/sessions/tags') return json(200, { tags: ['demo'] });
      return route.continue();
    });
    await login(conflict);
    await conflict.goto(`${baseURL}/#/sessions/fake-session`, { waitUntil: 'networkidle' });
    await conflict.waitForSelector('#structured-session');
    await conflict.click('#structured-session');
    await conflict.waitForSelector('.modal');
    assert((await conflict.locator('.modal').innerText()).includes('Open in native terminal'), 'conflict dialog did not explain the native terminal');
    assert(await conflict.locator('#conflict-force').count() === 1, 'forced resume option missing');
    assert(await conflict.locator('#conflict-readonly').count() === 1, 'read-only option missing');
    await conflict.click('[data-close]');
    await conflict.close();

    // Per-host notification overrides: the settings page must persist the
    // override and the reset must remove it.
    const overridePage = await desktop.newPage();
    let hostPut = null;
    let hostDeleted = false;
    await overridePage.route('**/api/v1/**', async (route) => {
      const request = route.request();
      const pathname = new URL(request.url()).pathname;
      const json = (status, body) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
      if (pathname === '/api/v1/hosts') {
        return json(200, { hosts: [{ id: 'host-1', name: 'demo-host', status: 'online', agents: [] }] });
      }
      if (pathname === '/api/v1/me/settings' && request.method() === 'GET') {
        return json(200, {
          theme: 'dark',
          notifications: { approval: true, completed: true, failed: true, disconnected: true },
          notifications_by_host: {},
        });
      }
      if (pathname === '/api/v1/me/settings/notifications/hosts/host-1' && request.method() === 'PUT') {
        hostPut = JSON.parse(request.postData() || '{}');
        return json(200, { host_id: 'host-1', notifications: hostPut.notifications });
      }
      if (pathname === '/api/v1/me/settings/notifications/hosts/host-1' && request.method() === 'DELETE') {
        hostDeleted = true;
        return json(200, { status: 'reset' });
      }
      return route.continue();
    });
    await login(overridePage);
    await overridePage.goto(`${baseURL}/#/settings`, { waitUntil: 'domcontentloaded' });
    await overridePage.waitForSelector('#settings-notify-host');
    assert((await overridePage.locator('#settings-host-notify-state').innerText()).includes('Using global defaults'), 'host override state did not start at the global default');
    await overridePage.uncheck('[data-host-notify-kind=failed]');
    await overridePage.waitForTimeout(400);
    assert(hostPut && hostPut.notifications.failed === false, 'host override was not persisted');
    await overridePage.click('#settings-host-notify-reset');
    await overridePage.waitForTimeout(400);
    assert(hostDeleted, 'host override reset did not reach the API');
    assert((await overridePage.locator('#settings-host-notify-state').innerText()).includes('Using global defaults'), 'reset did not restore the global state');
    await overridePage.close();

    const mobile = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const phone = await mobile.newPage();
    await login(phone);
    await phone.goto(`${baseURL}/#/settings`, { waitUntil: 'networkidle' });
    await phone.waitForSelector('[data-theme-choice=light]');
    const overflow = await phone.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
    assert(!overflow, 'mobile layout has horizontal overflow');
    assert(await phone.locator('.nav a[href="#/settings"]').isVisible(), 'settings navigation is not visible on mobile');
    assert(await phone.locator('.nav a[href="#/hosts"]').isVisible(), 'hosts navigation is not visible on mobile');
    await phone.goto(`${baseURL}/#/projects`, { waitUntil: 'networkidle' });
    assert(await phone.locator('#view').count(), 'projects view missing on mobile');
    await phone.goto(`${baseURL}/#/workspace`, { waitUntil: 'networkidle' });
    assert(await phone.locator('#view').count(), 'workspace view missing on mobile');
    const workspaceOverflow = await phone.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
    assert(!workspaceOverflow, 'workspace has horizontal overflow on mobile');
    await desktop.close();
    await mobile.close();
    console.log('browser smoke test passed');
  } finally {
    await browser.close();
    if (child) child.kill();
  }
}

run().catch((error) => {
  console.error(error);
  if (child) child.kill();
  process.exit(1);
});
