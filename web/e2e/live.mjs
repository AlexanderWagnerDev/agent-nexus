// Live end-to-end check against a real paired host and agent.
//
// Usage:
//   BASE_URL=http://127.0.0.1:8788 RUN_ID=<structured run id> npm run e2e:live
//
// Optional: E2E_USER, E2E_PASSWORD, LIVE_PROMPT, LIVE_EXPECT.
// This sends one real prompt to the agent and is intentionally not part of CI.
import { chromium } from 'playwright';

const baseURL = process.env.BASE_URL || 'http://127.0.0.1:8788';
const runID = process.env.RUN_ID || '';
const username = process.env.E2E_USER || 'owner';
const password = process.env.E2E_PASSWORD || 'correct-horse-battery';
const prompt = process.env.LIVE_PROMPT || 'Reply with exactly LIVE-OK and do not use tools.';
const expect = process.env.LIVE_EXPECT || 'LIVE-OK';

if (!runID) {
  console.error('RUN_ID is required: open a structured run and copy its run id');
  process.exit(2);
}

const browser = await chromium.launch(process.env.E2E_CHROME ? { executablePath: process.env.E2E_CHROME } : {});
try {
  const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto(`${baseURL}/`, { waitUntil: 'domcontentloaded' });
  if (await page.locator('#login-form').count()) {
    await page.fill('[name=username]', username);
    await page.fill('[name=password]', password);
    await page.click('#login-form button[type=submit]');
    await page.waitForTimeout(1200);
  }
  await page.goto(`${baseURL}/#/runs/${runID}`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('#prompt-text', { timeout: 30000 });
  if (!(await page.locator('#opt-model-btn').count())) {
    throw new Error('this run does not expose a structured model selector');
  }
  await page.fill('#prompt-text', prompt);
  await page.click('#send-prompt');
  await page.waitForFunction(
    (needle) => [...document.querySelectorAll('.turn-assistant')].some((node) => node.innerText.includes(needle)),
    expect,
    { timeout: 180000 },
  );
  if (errors.length) throw new Error(`page errors: ${errors.join('; ')}`);
  console.log(`live run ${runID} answered with ${expect}`);
} finally {
  await browser.close();
}
