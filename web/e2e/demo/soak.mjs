// Load/soak harness for the hub. Starts a disposable hub, pairs several
// synthetic demo hosts, opens structured runs and samples hub metrics for the
// requested duration. No real store, agent account or transcript is touched.
//
//   SOAK_MINUTES=1 node web/e2e/demo/soak.mjs
//   SOAK_MINUTES=4320 SOAK_HOSTS=6 SOAK_RUNS=24 node web/e2e/demo/soak.mjs
//
// Env: SOAK_PORT (8792), SOAK_HOSTS (3), SOAK_RUNS (8), SOAK_INTERVAL_MS (5000),
//      SOAK_OUT (soak-<timestamp>.csv), SOAK_STRICT=1 to fail on growth.
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import process from 'node:process';

const port = Number(process.env.SOAK_PORT || 8792);
const base = `http://127.0.0.1:${port}`;
const minutes = Math.max(0.1, Number(process.env.SOAK_MINUTES || 1));
const hostCount = Math.max(1, Math.min(12, Number(process.env.SOAK_HOSTS || 3)));
const runCount = Math.max(0, Number(process.env.SOAK_RUNS || 8));
const interval = Math.max(1000, Number(process.env.SOAK_INTERVAL_MS || 5000));
const strict = process.env.SOAK_STRICT === '1';
const exe = process.platform === 'win32' ? '.exe' : '';
const repoRoot = path.resolve(import.meta.dirname, '..', '..', '..');
const hubBin = path.resolve(process.env.DEMO_HUB_BIN || path.join(repoRoot, 'bin', `nexus-hub${exe}`));
const demoBin = path.resolve(process.env.DEMO_BIN || path.join(repoRoot, 'bin', `nexus-demo${exe}`));
const root = path.resolve(process.env.SOAK_ROOT || path.join(os.tmpdir(), `agentnexus-soak-${Date.now()}`));
const dataDir = path.join(root, 'hub');
const configDir = path.join(root, 'config');
const password = 'demo-alpha-only';
fs.mkdirSync(dataDir, { recursive: true });
fs.mkdirSync(configDir, { recursive: true });

const children = [];
function track(child, name) {
  children.push(child);
  child.on('exit', (code) => { if (code) console.error(`[${name}] exit ${code}`); });
  return child;
}
const hub = track(spawn(hubBin, ['-addr', `127.0.0.1:${port}`, '-data-dir', dataDir, '-metrics', '-metrics-token', 'soak', '-log-level', 'error'], { stdio: ['ignore', 'pipe', 'pipe'] }), 'hub');
hub.stderr.on('data', (chunk) => process.stderr.write(`[hub] ${chunk}`));

let cookie = '';
async function api(pathname, options = {}) {
  const res = await fetch(base + pathname, {
    ...options,
    headers: { 'Content-Type': 'application/json', 'X-Nexus-Request': '1', ...(cookie ? { Cookie: cookie } : {}), ...(options.headers || {}) },
  });
  const setCookie = res.headers.get('set-cookie');
  if (setCookie) cookie = setCookie.split(';')[0];
  const text = await res.text();
  if (!res.ok) throw new Error(`${options.method || 'GET'} ${pathname}: ${res.status} ${text}`);
  return text ? JSON.parse(text) : {};
}
async function waitHealthy() {
  for (let i = 0; i < 150; i += 1) {
    try { if ((await fetch(base + '/healthz')).ok) return; } catch { /* starting */ }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error('hub did not become healthy');
}
function runOnce(args) {
  return new Promise((resolve, reject) => {
    const child = spawn(demoBin, args, { stdio: ['ignore', 'pipe', 'pipe'] });
    let out = '';
    child.stdout.on('data', (chunk) => { out += chunk; });
    child.stderr.on('data', (chunk) => process.stderr.write(chunk));
    child.on('exit', (code) => (code === 0 ? resolve(out) : reject(new Error(`demo exited ${code}`))));
  });
}
function metric(text, name) {
  const match = new RegExp(`^${name} (\\d+)$`, 'm').exec(text);
  return match ? Number(match[1]) : 0;
}

let rows = [];
try {
  await waitHealthy();
  const status = await api('/api/v1/setup/status');
  if (!status.setup) await api('/api/v1/setup', { method: 'POST', body: JSON.stringify({ username: 'demo', password }) });
  else await api('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ username: 'demo', password }) });
  await api('/api/v1/auth/step-up/password', { method: 'POST', body: JSON.stringify({ password }) });

  const scenarios = ['windows', 'linux', 'mac'];
  for (let i = 0; i < hostCount; i += 1) {
    const name = `soak-host-${i + 1}`;
    const config = path.join(configDir, `${name}.json`);
    const { code } = await api('/api/v1/hosts/pairing-codes', { method: 'POST', body: '{}' });
    const out = await runOnce(['pair', '--hub', base, '--code', code, '--name', name, '--config', config]);
    const hostID = /HOST=(\S+)/.exec(out)?.[1];
    const sas = /SAS=(\S+)/.exec(out)?.[1];
    if (!hostID || !sas) throw new Error(`pair output missing host/sas for ${name}`);
    await api(`/api/v1/hosts/${hostID}/confirm`, { method: 'POST', body: JSON.stringify({ sas }) });
    track(spawn(demoBin, ['serve', '--config', config, '--scenario', scenarios[i % scenarios.length]], { stdio: ['ignore', 'pipe', 'pipe'] }), name);
  }
  await new Promise((resolve) => setTimeout(resolve, 1500));

  const { sessions } = await api('/api/v1/sessions?limit=200');
  for (const session of sessions.slice(0, runCount)) {
    await api(`/api/v1/sessions/${session.id}/runs`, { method: 'POST', body: JSON.stringify({ mode: 'structured', cols: 120, rows: 32 }) });
  }

  const deadline = Date.now() + minutes * 60 * 1000;
  while (Date.now() < deadline) {
    const metrics = await (await fetch(base + '/metrics', { headers: { Authorization: 'Bearer soak' } })).text();
    const [hostData, sessionData, runData] = await Promise.all([
      api('/api/v1/hosts'), api('/api/v1/sessions?limit=500'), api('/api/v1/runs'),
    ]);
    rows.push({
      at: new Date().toISOString(),
      hosts: hostData.hosts.length,
      sessions: sessionData.sessions.length,
      runs: runData.runs.length,
      goroutines: metric(metrics, 'nexus_goroutines'),
      heapBytes: metric(metrics, 'nexus_heap_alloc_bytes'),
      fds: metric(metrics, 'nexus_open_fds'),
    });
    await new Promise((resolve) => setTimeout(resolve, interval));
  }

  const out = path.resolve(process.env.SOAK_OUT || `soak-${new Date().toISOString().replace(/[:.]/g, '-')}.csv`);
  fs.writeFileSync(out, 'at,hosts,sessions,runs,goroutines,heap_bytes,fds\n' + rows.map((row) => Object.values(row).join(',')).join('\n') + '\n');
  const first = rows[0] || {};
  const last = rows[rows.length - 1] || {};
  console.log(`samples: ${rows.length}, ${minutes} min, ${out}`);
  console.log(`goroutines ${first.goroutines} -> ${last.goroutines}, heap ${first.heapBytes} -> ${last.heapBytes}, fds ${first.fds} -> ${last.fds}`);
  const goroutineGrowth = (last.goroutines || 0) - (first.goroutines || 0);
  const heapGrowth = (last.heapBytes || 0) - (first.heapBytes || 0);
  if (strict && (goroutineGrowth > 50 || heapGrowth > 64 * 1024 * 1024)) {
    throw new Error(`leak suspicion: goroutines +${goroutineGrowth}, heap +${heapGrowth}`);
  }
} catch (err) {
  console.error(err);
  process.exitCode = 1;
} finally {
  for (const child of children) child.kill();
}
