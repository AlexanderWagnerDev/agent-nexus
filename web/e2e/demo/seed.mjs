// Seed a throwaway hub with synthetic hosts, sessions and runs for screenshots
// and UI development. Uses only demo data; never points at a real data dir.
//
//   node web/e2e/demo/seed.mjs
//
// Env:
//   DEMO_HUB_BIN   default ../../tmp is not assumed; set explicitly if needed
//   DEMO_DATA      data directory (required, must not exist or be disposable)
//   DEMO_PORT      default 8790
//   DEMO_PASSWORD  default demo-alpha-only
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import process from 'node:process';

const port = Number(process.env.DEMO_PORT || 8790);
const base = `http://127.0.0.1:${port}`;
const demoRoot = path.resolve(process.env.DEMO_ROOT || path.join(os.tmpdir(), 'agentnexus-demo'));
const dataDir = path.join(demoRoot, 'hub');
const configDir = path.join(demoRoot, 'config');
const exe = process.platform === 'win32' ? '.exe' : '';
const hubBin = path.resolve(process.env.DEMO_HUB_BIN || `bin/nexus-hub${exe}`);
const demoBin = path.resolve(process.env.DEMO_BIN || `bin/nexus-demo${exe}`);
const password = process.env.DEMO_PASSWORD || 'demo-alpha-only';
const user = 'demo';

if (!dataDir.startsWith(demoRoot + path.sep)) {
  console.error('Refusing to seed outside the demo root.');
  process.exit(1);
}
fs.rmSync(dataDir, { recursive: true, force: true });
fs.rmSync(configDir, { recursive: true, force: true });
fs.mkdirSync(dataDir, { recursive: true });
fs.mkdirSync(configDir, { recursive: true });

const children = [];
function track(child, name) {
  children.push({ child, name });
  child.on('exit', (code) => console.log(`[${name}] exit ${code}`));
  return child;
}

const hub = track(spawn(hubBin, ['-addr', `127.0.0.1:${port}`, '-data-dir', dataDir, '-log-level', 'warn'], {
  stdio: ['ignore', 'pipe', 'pipe'],
}), 'hub');
hub.stderr.on('data', (d) => process.stderr.write(`[hub] ${d}`));

let cookie = '';
async function api(pathname, options = {}) {
  const res = await fetch(base + pathname, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      'X-Nexus-Request': '1',
      ...(cookie ? { Cookie: cookie } : {}),
      ...(options.headers || {}),
    },
  });
  const setCookie = res.headers.get('set-cookie');
  if (setCookie) cookie = setCookie.split(';')[0];
  const text = await res.text();
  let body = null;
  try { body = text ? JSON.parse(text) : null; } catch { body = text; }
  if (!res.ok) throw new Error(`${options.method || 'GET'} ${pathname}: ${res.status} ${body?.error || text}`);
  return body;
}

async function waitHealthy() {
  for (let i = 0; i < 100; i += 1) {
    try {
      const res = await fetch(base + '/healthz');
      if (res.ok) return;
    } catch { /* starting */ }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error('hub did not become healthy');
}

function runOnce(args) {
  return new Promise((resolve, reject) => {
    const child = spawn(demoBin, args, { stdio: ['ignore', 'pipe', 'pipe'], env: { ...process.env } });
    let out = '';
    child.stdout.on('data', (d) => { out += d; });
    child.stderr.on('data', (d) => process.stderr.write(d));
    child.on('exit', (code) => (code === 0 ? resolve(out) : reject(new Error(`demo exited ${code}`))));
  });
}

const hosts = [
  { scenario: 'windows', name: 'windows-dev' },
  { scenario: 'linux', name: 'linux-builder' },
  { scenario: 'mac', name: 'macbook-demo' },
];

try {
  await waitHealthy();
  const status = await api('/api/v1/setup/status');
  if (!status.setup) {
    await api('/api/v1/setup', { method: 'POST', body: JSON.stringify({ username: user, password }) });
  } else {
    await api('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ username: user, password }) });
  }
  await api('/api/v1/auth/step-up/password', { method: 'POST', body: JSON.stringify({ password }) });

  for (const host of hosts) {
    const config = path.join(configDir, `${host.name}.json`);
    const { code } = await api('/api/v1/hosts/pairing-codes', { method: 'POST', body: '{}' });
    const out = await runOnce(['pair', '--hub', base, '--code', code, '--name', host.name, '--config', config]);
    const hostID = /HOST=(\S+)/.exec(out)?.[1];
    const sas = /SAS=(\S+)/.exec(out)?.[1];
    if (!hostID || !sas) throw new Error(`pair output missing host/sas for ${host.name}: ${out}`);
    await api(`/api/v1/hosts/${hostID}/confirm`, { method: 'POST', body: JSON.stringify({ sas }) });
    track(spawn(demoBin, ['serve', '--config', config, '--scenario', host.scenario], {
      stdio: ['ignore', 'pipe', 'pipe'],
    }), host.name);
    console.log(`paired ${host.name} (${hostID})`);
  }

  await new Promise((r) => setTimeout(r, 1200));
  const { sessions } = await api('/api/v1/sessions?limit=50');
  const wanted = [/upload retries/i, /pagination/i, /flaky checkout/i];
  const chosen = wanted
    .map((re) => sessions.find((s) => re.test(s.title)))
    .filter(Boolean);
  for (const session of chosen) {
    const run = await api(`/api/v1/sessions/${session.id}/runs`, {
      method: 'POST',
      body: JSON.stringify({ mode: 'structured', cols: 120, rows: 32 }),
    });
    console.log(`started demo run ${run.id} for ${session.title}`);
  }
  console.log(`READY ${base} (login ${user} / ${password})`);

  if (process.env.DEMO_VERIFY === '1') {
    let allRuns = [];
    let notifications = [];
    for (let i = 0; i < 50; i += 1) {
      allRuns = (await api('/api/v1/runs')).runs || [];
      notifications = (await api('/api/v1/notifications?limit=40')).notifications || [];
      if (notifications.some((n) => n.kind === 'failed') && allRuns.filter((r) => r.state === 'idle').length >= 2) break;
      await new Promise((r) => setTimeout(r, 200));
    }
    const { hosts: paired } = await api('/api/v1/hosts');
    if (paired.length !== 3) throw new Error(`expected 3 hosts, got ${paired.length}`);
    if (paired.some((h) => h.status !== 'online')) throw new Error('a demo host is offline');
    const platforms = new Set(paired.map((h) => h.os));
    if (!platforms.has('windows') || !platforms.has('linux') || !platforms.has('darwin')) {
      throw new Error(`platforms not distinct: ${[...platforms].join(',')}`);
    }
    const { sessions: allSessions } = await api('/api/v1/sessions?limit=50');
    if (allSessions.length < 7) throw new Error(`expected at least 7 sessions, got ${allSessions.length}`);
    if (allRuns.length < 2) throw new Error(`expected at least 2 active demo runs, got ${allRuns.length}`);
    if (!notifications.some((n) => n.kind === 'failed')) throw new Error('expected a failed-run notification');
    const sample = allSessions.find((s) => s.repo_url);
    if (!sample) throw new Error('no session with repository metadata');
    const git = await api(`/api/v1/sessions/${sample.id}/git`);
    if (!git.diff || !git.branch) throw new Error('demo git fixture missing');
    console.log('VERIFY OK 3 hosts, 7+ sessions, 2 active runs, failed-run notification, git fixture');
    for (const { child } of children) child.kill();
    process.exit(0);
  }
} catch (err) {
  console.error(err);
  for (const { child } of children) child.kill();
  process.exit(1);
}

const shutdown = () => {
  for (const { child } of children) child.kill();
  process.exit(0);
};
process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
