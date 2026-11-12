// WCAG 2.2 A/AA scan with axe-core against a running hub.
//
// Usage: BASE_URL=http://127.0.0.1:8788 npm run e2e:a11y
// Optional RUN_ID scans a structured chat page.
import { createRequire } from 'node:module';
import { chromium } from 'playwright';

const require = createRequire(import.meta.url);
const axePath = require.resolve('axe-core/axe.min.js');
const baseURL = process.env.BASE_URL || 'http://127.0.0.1:8788';
const username = process.env.E2E_USER || 'owner';
const password = process.env.E2E_PASSWORD || 'correct-horse-battery';
const runID = process.env.RUN_ID || '';
const lang = process.env.E2E_LANG || '';

async function login(page) {
  await page.goto(`${baseURL}/`, { waitUntil: 'domcontentloaded' });
  if (await page.locator('#login-form').count()) {
    await page.fill('[name=username]', username);
    await page.fill('[name=password]', password);
    await page.click('#login-form button[type=submit]');
    await page.waitForTimeout(1200);
  }
}

async function scan(page, label) {
  if (!page.__axeRouted) {
    await page.route('**/__axe.js', (route) => route.fulfill({ path: axePath, contentType: 'application/javascript' }));
    page.__axeRouted = true;
  }
  await page.addScriptTag({ url: '/__axe.js' });
  const results = await page.evaluate(async () => {
    return window.axe.run(document, {
      runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'] },
      resultTypes: ['violations'],
    });
  });
  const violations = results.violations.map((v) => ({
    id: v.id, impact: v.impact, help: v.help,
    nodes: v.nodes.slice(0, 4).map((n) => ({ target: n.target.join(' '), summary: n.failureSummary, html: n.html })),
  }));
  console.log(JSON.stringify({ page: label, violations }));
  return violations;
}

const browser = await chromium.launch(process.env.E2E_CHROME ? { executablePath: process.env.E2E_CHROME } : {});
async function newContext(options) {
  const context = await browser.newContext(options);
  if (lang) await context.addInitScript((value) => localStorage.setItem('nexus-lang', value), lang);
  return context;
}
let failures = 0;
try {
  const desktop = await newContext({ viewport: { width: 1440, height: 900 } });
  const page = await desktop.newPage();
  await login(page);
  const routes = [
    ['#/', 'home'], ['#/projects', 'projects'], ['#/sessions', 'sessions'],
    ['#/usage', 'usage'], ['#/runs', 'runs'], ['#/hosts', 'hosts'], ['#/settings', 'settings'],
  ];
  if (runID) routes.push([`#/runs/${runID}`, 'chat']);
  for (const [route, label] of routes) {
    await page.goto(`${baseURL}/${route}`, { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(800);
    const violations = await scan(page, `desktop:${label}`);
    failures += violations.filter((v) => v.impact === 'critical' || v.impact === 'serious').length;
  }
  await desktop.close();

  const mobile = await newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  const phone = await mobile.newPage();
  await login(phone);
  for (const [route, label] of [['#/', 'home'], ['#/sessions', 'sessions'], ['#/usage', 'usage'], ['#/settings', 'settings']]) {
    await phone.goto(`${baseURL}/${route}`, { waitUntil: 'domcontentloaded' });
    await phone.waitForTimeout(800);
    const violations = await scan(phone, `mobile:${label}`);
    failures += violations.filter((v) => v.impact === 'critical' || v.impact === 'serious').length;
  }
  await mobile.close();
  if (failures > 0) {
    console.error(`${failures} critical/serious WCAG violations`);
    process.exitCode = 1;
  } else {
    console.log('WCAG scan: no critical/serious violations');
  }
} finally {
  await browser.close();
}
