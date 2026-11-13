// Virtualized session-list regression test with a 10,000-session mock.
// Starts a throwaway hub when E2E_SPAWN_HUB=1; otherwise uses BASE_URL.
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { chromium } from 'playwright';
import { chromiumLaunchOptions } from './launch.mjs';

const root = path.resolve(import.meta.dirname, '..', '..');
const port = process.env.E2E_VIRTUAL_PORT || '18789';
const base = process.env.BASE_URL || `http://127.0.0.1:${port}`;
const username = process.env.E2E_USER || 'virtual-user';
const password = process.env.E2E_PASSWORD || 'virtual-alpha-only';
const TOTAL = 10000;
const PAGE = 200;

let child = null;
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function waitFor(url) {
  for (let i = 0; i < 100; i++) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch { /* retry */ }
    await sleep(150);
  }
  throw new Error(`hub did not start at ${url}`);
}

async function startHub() {
  if (process.env.E2E_SPAWN_HUB !== '1') return;
  const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'agentnexus-virtual-'));
  const bin = process.env.NEXUS_HUB_BIN || path.join(root, 'bin', process.platform === 'win32' ? 'nexus-hub.exe' : 'nexus-hub');
  child = spawn(bin, ['--addr', `127.0.0.1:${port}`, '--data-dir', dataDir,
    '--public-url', base, '--log-level', 'error'], { stdio: 'inherit' });
  await waitFor(`${base}/healthz`);
}

async function ensureSetup() {
  const status = await (await fetch(`${base}/api/v1/setup/status`)).json();
  if (!status.setup_required) return;
  const response = await fetch(`${base}/api/v1/setup`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Nexus-Request': '1' },
    body: JSON.stringify({ username, password }),
  });
  if (!response.ok) throw new Error(`setup failed: ${response.status} ${await response.text()}`);
}

function mockSessions() {
  const now = Date.now();
  const agents = ['opencode', 'codex', 'claude'];
  return Array.from({ length: TOTAL }, (_, i) => {
    const project = i % 50;
    return {
      id: `virtual-session-${i}`,
      host_id: `host-${i % 3}`,
      host_name: `demo-${i % 3}`,
      agent: agents[i % agents.length],
      native_id: `native-${i}`,
      title: i % 7 === 0
        ? `A deliberately long session title for row ${i} that must be truncated rather than wrapping the virtual row`
        : `Session ${i} in project ${project}`,
      local_title: i % 11 === 0 ? `Local title ${i}` : '',
      cwd: `/work/project-${project}`,
      repo_url: `https://github.com/example/project-${project}`,
      repo_root: `/work/project-${project}`,
      git_branch: i % 2 === 0 ? 'main' : 'feature/x',
      updated_at: new Date(now - i * 60000).toISOString(),
      created_at: new Date(now - i * 120000).toISOString(),
      message_count: i % 20,
      state: i % 13 === 0 ? 'running' : 'idle',
      active_elsewhere: false,
      tags: i % 17 === 0 ? ['demo'] : [],
    };
  });
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

async function run() {
  await startHub();
  await ensureSetup();
  const browser = await chromium.launch(chromiumLaunchOptions());
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (error) => errors.push(String(error)));
    const sessions = mockSessions();
    let requests = 0;
    let pages = 0;
    await page.route('**/api/v1/sessions?*', async (route) => {
      const url = new URL(route.request().url());
      const limit = Math.min(Number(url.searchParams.get('limit') || PAGE), PAGE);
      const offset = Number(url.searchParams.get('offset') || 0);
      const slice = sessions.slice(offset, offset + limit);
      requests++;
      pages = Math.max(pages, offset / limit + 1);
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          sessions: slice, has_more: offset + limit < TOTAL, offset, total: TOTAL,
        }),
      });
    });

    await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('#login-form, .nav', { timeout: 15000 });
    if (await page.locator('#login-form').count()) {
      await page.fill('[name=username]', username);
      await page.fill('[name=password]', password);
      await page.click('#login-form button[type=submit]');
      await page.waitForSelector('.nav', { timeout: 15000 });
      await page.waitForTimeout(600);
    }

    const started = Date.now();
    await page.goto(base + '/#/sessions', { waitUntil: 'domcontentloaded' });
    try {
      await page.waitForSelector('#session-list .virtual-row', { timeout: 20000 });
    } catch (err) {
      throw new Error(`${err.message}\npage errors: ${errors.join('; ')}\nbody: ${(await page.locator('body').innerText()).slice(0, 500)}`);
    }
    const firstRenderMs = Date.now() - started;
    await page.waitForFunction(() => document.getElementById('session-count')?.textContent?.includes('10000'), null, { timeout: 10000 });

    const initial = await page.evaluate(() => {
      const list = document.getElementById('session-list');
      return {
        rows: list.querySelectorAll('.virtual-row').length,
        nodes: list.querySelectorAll('*').length,
        count: document.getElementById('session-count').textContent,
        scrollTop: list.scrollTop,
      };
    });
    assert(initial.rows > 0 && initial.rows < 80, `unexpected virtual row count: ${initial.rows}`);
    assert(initial.nodes < 400, `too many DOM nodes in the virtual list: ${initial.nodes}`);
    assert(initial.count.includes('200') && initial.count.includes('10000'), `unexpected count: ${initial.count}`);

    // Scroll to the bottom a few times; every page must append without
    // duplicating rows and without growing the DOM.
    let loaded = 200;
    for (let i = 0; i < 6; i++) {
      await page.evaluate(() => {
        const list = document.getElementById('session-list');
        list.scrollTop = list.scrollHeight;
      });
      await page.waitForTimeout(350);
      const text = await page.locator('#session-count').textContent();
      const match = text.match(/(\d+)\s+of/);
      if (match) loaded = Number(match[1]);
    }
    assert(loaded > 200, `scrolling did not load another page: ${loaded}`);

    const after = await page.evaluate(() => {
      const list = document.getElementById('session-list');
      const hrefs = [...list.querySelectorAll('.virtual-row')].map((row) => row.getAttribute('href'));
      return {
        rows: list.querySelectorAll('.virtual-row').length,
        nodes: list.querySelectorAll('*').length,
        unique: new Set(hrefs).size,
        total: hrefs.length,
        scrollTop: list.scrollTop,
      };
    });
    assert(after.rows > 0 && after.rows < 80, `row window grew: ${after.rows}`);
    assert(after.nodes < 400, `DOM grew beyond the window: ${after.nodes}`);
    assert(after.unique === after.total, `duplicate rendered rows: ${after.unique}/${after.total}`);

    // A resize re-renders the window without resetting the scroll position.
    const beforeResize = await page.evaluate(() => document.getElementById('session-list').scrollTop);
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.waitForTimeout(250);
    const afterResize = await page.evaluate(() => document.getElementById('session-list').scrollTop);
    assert(Math.abs(afterResize - beforeResize) < 2, `scroll position jumped: ${beforeResize} -> ${afterResize}`);

    // Mobile stays within the viewport and keeps a small window.
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(base + '/#/sessions', { waitUntil: 'domcontentloaded' });
    try {
      await page.waitForSelector('#session-list .virtual-row', { timeout: 20000 });
    } catch (err) {
      throw new Error(`${err.message}\npage errors: ${errors.join('; ')}\nbody: ${(await page.locator('body').innerText()).slice(0, 500)}`);
    }
    const mobile = await page.evaluate(() => ({
      rows: document.getElementById('session-list').querySelectorAll('.virtual-row').length,
      overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    }));
    assert(!mobile.overflow, 'the mobile sessions page scrolls horizontally');
    assert(mobile.rows > 0 && mobile.rows < 80, `mobile virtual rows: ${mobile.rows}`);
    assert(errors.length === 0, `page errors: ${errors.join('; ')}`);

    console.log(JSON.stringify({
      total: TOTAL, firstRenderMs, requests, pages, initialRows: initial.rows,
      initialNodes: initial.nodes, afterRows: after.rows, afterNodes: after.nodes,
      loadedAfterScroll: loaded, mobileRows: mobile.rows,
    }, null, 2));
  } finally {
    await browser.close();
  }
}

try {
  await run();
} finally {
  if (child) child.kill();
}
