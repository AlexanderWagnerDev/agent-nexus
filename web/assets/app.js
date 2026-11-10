/* AgentNexus browser client — dependency-free ES modules. */
const app = document.getElementById('app');
const modalRoot = document.getElementById('modal-root');
const toastRoot = document.getElementById('toast-root');

const state = {
  me: null,
  setupRequired: false,
  hubName: 'AgentNexus',
  ws: null,
  wsRetry: 1000,
  wsGen: 0,
  wsHandlers: new Set(),
  pageCleanup: null,
  run: null,
};

/* ---------- helpers ---------- */

function esc(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function fmtTime(value) {
  if (!value) return '—';
  const d = new Date(value);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1970) return '—';
  return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

function fmtRelative(value) {
  if (!value) return '—';
  const d = new Date(value);
  const diff = Date.now() - d.getTime();
  if (Number.isNaN(diff)) return '—';
  const s = Math.round(diff / 1000);
  if (s < 45) return 'just now';
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h ago`;
  const days = Math.round(h / 24);
  if (days < 30) return `${days}d ago`;
  return new Date(value).toLocaleDateString();
}

function shortId(id) {
  return id ? id.slice(0, 10) : '';
}

function badge(text, cls = '') {
  return `<span class="badge ${esc(cls)}">${esc(text)}</span>`;
}

function stateBadge(s) {
  const map = {
    running: 'running', waiting_for_input: 'waiting_for_input', idle: 'idle',
    completed: 'completed', failed: 'failed', unknown: 'unknown',
    starting: 'running', stopping: 'waiting_for_input', exited: 'completed',
  };
  return badge((s || 'unknown').replaceAll('_', ' '), map[s] || 'unknown');
}

function agentBadge(agent) {
  return `<span class="badge agent no-dot">${esc(agent || 'agent')}</span>`;
}

function toast(message, isError = false) {
  const node = document.createElement('div');
  node.className = `toast ${isError ? 'error' : ''}`;
  node.textContent = message;
  toastRoot.appendChild(node);
  setTimeout(() => node.remove(), 4200);
}

function copyText(text) {
  navigator.clipboard?.writeText(text).then(() => toast('Copied to clipboard')).catch(() => toast('Could not copy', true));
}

async function api(path, options = {}) {
  const { method = 'GET', body, allow401 = false } = options;
  const headers = { 'X-Nexus-Request': '1', Accept: 'application/json' };
  const init = { method, headers, credentials: 'same-origin' };
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  const res = await fetch('/api/v1' + path, init);
  const text = await res.text();
  let data = {};
  if (text) {
    try { data = JSON.parse(text); } catch { data = { error: text }; }
  }
  if (!res.ok) {
    const err = new Error(data.error || `${res.status} ${res.statusText}`);
    err.status = res.status;
    if (res.status === 401 && !allow401) {
      state.me = null;
      disconnectWS();
      renderAuth('login');
    }
    throw err;
  }
  return data;
}

function b64encode(bytes) {
  let s = '';
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s);
}

function b64decode(value) {
  const bin = atob(value);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function modal(html) {
  modalRoot.innerHTML = `<div class="modal-backdrop" data-modal-backdrop><div class="modal" role="dialog" aria-modal="true">${html}</div></div>`;
  const backdrop = modalRoot.firstElementChild;
  backdrop.addEventListener('click', (e) => {
    if (e.target === backdrop) closeModal();
  });
  modalRoot.querySelectorAll('[data-close]').forEach((b) => b.addEventListener('click', closeModal));
}

function closeModal() { modalRoot.innerHTML = ''; }

function confirmDanger(message, onConfirm) {
  modal(`
    <h2>Are you sure?</h2>
    <p class="hint">${esc(message)}</p>
    <div class="modal-actions">
      <button class="btn ghost" type="button" data-close>Cancel</button>
      <button class="btn danger" type="button" id="confirm-danger">Confirm</button>
    </div>`);
  document.getElementById('confirm-danger').addEventListener('click', async () => {
    closeModal();
    await onConfirm();
  });
}

/* ---------- websocket ---------- */

function connectWS() {
  if (!state.me) return;
  if (state.ws && (state.ws.readyState === WebSocket.OPEN || state.ws.readyState === WebSocket.CONNECTING)) return;
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${proto}://${location.host}/api/v1/ws`);
  state.ws = ws;
  ws.addEventListener('open', () => { state.wsRetry = 1000; state.wsGen++; });
  ws.addEventListener('message', (event) => {
    let msg;
    try { msg = JSON.parse(event.data); } catch { return; }
    if (msg.type === 'ready') return;
    for (const handler of state.wsHandlers) {
      try { handler(msg); } catch (err) { console.error(err); }
    }
  });
  ws.addEventListener('close', () => {
    state.ws = null;
    if (!state.me) return;
    const delay = Math.min(state.wsRetry, 15000);
    state.wsRetry = Math.min(state.wsRetry * 1.7, 15000);
    setTimeout(connectWS, delay);
  });
  ws.addEventListener('error', () => ws.close());
}

function disconnectWS() {
  if (state.ws) {
    state.ws.onclose = null;
    state.ws.close();
    state.ws = null;
  }
}

function wsSend(msg) {
  if (state.ws?.readyState === WebSocket.OPEN) {
    state.ws.send(JSON.stringify(msg));
    return true;
  }
  return false;
}

/* ---------- shell ---------- */

const navItems = [
  ['#/', 'Overview', '◈', 'overview'],
  ['#/hosts', 'Hosts', '▤', 'hosts'],
  ['#/sessions', 'Sessions', '▦', 'sessions'],
  ['#/search', 'Search', '⌕', 'search'],
  ['#/devices', 'Devices', '◉', 'devices'],
  ['#/audit', 'Audit', '☰', 'audit'],
];

function renderShell(active = 'overview') {
  const initials = (state.me?.username || '?').slice(0, 1).toUpperCase();
  app.className = '';
  app.innerHTML = `
    <div class="layout">
      <aside class="sidebar">
        <div class="brand">
          <div class="brand-mark">A</div>
          <div><strong>AgentNexus</strong><small>${esc(state.hubName)}</small></div>
        </div>
        <nav class="nav">
          ${navItems.map(([href, label, icon, key]) => `
            <a href="${href}" class="${key === active ? 'active' : ''}">
              <span class="icon" aria-hidden="true">${icon}</span><span>${label}</span>
            </a>`).join('')}
        </nav>
        <div class="sidebar-foot">
          <div class="user-chip"><span class="avatar">${esc(initials)}</span><span>${esc(state.me.username)}</span></div>
          <button class="btn ghost small" id="logout">Sign out</button>
        </div>
      </aside>
      <main class="main">
        <header class="topbar">
          <div><h1 id="page-title">Overview</h1><div class="sub" id="page-sub"></div></div>
          <div class="topbar-actions" id="page-actions"></div>
        </header>
        <div class="view" id="view"></div>
      </main>
    </div>`;
  document.getElementById('logout').addEventListener('click', logout);
  connectWS();
}

function setPage(title, sub = '', actions = '') {
  document.getElementById('page-title').textContent = title;
  document.getElementById('page-sub').textContent = sub;
  document.getElementById('page-actions').innerHTML = actions;
  return document.getElementById('view');
}

function activeFromHash() {
  const h = location.hash || '#/';
  if (h.startsWith('#/hosts')) return 'hosts';
  if (h.startsWith('#/sessions') || h.startsWith('#/runs')) return 'sessions';
  if (h.startsWith('#/search')) return 'search';
  if (h.startsWith('#/devices')) return 'devices';
  if (h.startsWith('#/audit')) return 'audit';
  return 'overview';
}

/* ---------- auth ---------- */

function authFrame(inner, subtitle) {
  app.className = 'app-loading';
  app.innerHTML = `
    <div class="auth-wrap">
      <div class="auth-card">
        <div class="auth-brand">
          <div class="brand-mark">A</div>
          <div><h1>AgentNexus</h1><p>${esc(subtitle || 'Your agents. Your machines. One place.')}</p></div>
        </div>
        <div class="card">${inner}</div>
        <p class="small muted" style="text-align:center;margin-top:14px">Self-hosted · no telemetry · transcript content stays on your hosts</p>
      </div>
    </div>`;
}

function renderAuth(mode, error = '') {
  state.me = null;
  disconnectWS();
  if (mode === 'setup') {
    authFrame(`
      <h2 class="mt-0">Create the owner account</h2>
      <p class="hint mb-2">This hub has no users yet. Choose a strong password; you can enroll a passkey or authenticator after signing in.</p>
      ${error ? `<div class="notice error mb-2">${esc(error)}</div>` : ''}
      <form class="form" id="setup-form">
        <label class="label">Username<input class="input" name="username" aria-label="Username" autocomplete="username" minlength="3" maxlength="64" required></label>
        <label class="label">Password<input class="input" name="password" aria-label="Password" type="password" autocomplete="new-password" minlength="10" required></label>
        <button class="btn primary block" type="submit" id="auth-submit">Create hub</button>
      </form>`, 'First-run setup');
    document.getElementById('setup-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const form = new FormData(event.target);
      const button = document.getElementById('auth-submit');
      button.disabled = true;
      button.innerHTML = '<span class="spinner"></span> Creating...';
      try {
        const data = await api('/setup', { method: 'POST', body: {
          username: form.get('username'), password: form.get('password'),
        }, allow401: true });
        state.me = await api('/me');
        state.hubName = state.me.hub_name || state.hubName;
        renderShell('overview');
        await route();
        if (data.totp_secret) {
          modal(`
            <h2>Save your authenticator secret</h2>
            <p class="hint">TOTP is optional until you confirm it. Scan this URI with an authenticator app, or copy the secret. You can enrol later in <b>Devices</b>.</p>
            <div class="secret">${esc(data.totp_secret)}</div>
            <p class="small muted mt-1">otpauth URI</p>
            <div class="secret">${esc(data.totp_url)}</div>
            <div class="modal-actions">
              <button class="btn" type="button" id="copy-totp">Copy secret</button>
              <button class="btn primary" type="button" data-close>I saved it</button>
            </div>`);
          document.getElementById('copy-totp').addEventListener('click', () => copyText(data.totp_secret));
        }
      } catch (err) {
        renderAuth('setup', err.message);
      }
    });
    return;
  }

  authFrame(`
    <h2 class="mt-0">Sign in</h2>
    <p class="hint mb-2">Use your owner account. Authenticator code is required once TOTP is enabled.</p>
    ${error ? `<div class="notice error mb-2">${esc(error)}</div>` : ''}
    <form class="form" id="login-form">
      <label class="label">Username<input class="input" name="username" aria-label="Username" autocomplete="username" required autofocus></label>
      <label class="label">Password<input class="input" name="password" aria-label="Password" type="password" autocomplete="current-password" required></label>
      <label class="label">Authenticator code <span class="muted">(if enabled)</span><input class="input" name="totp" aria-label="Authenticator code" inputmode="numeric" autocomplete="one-time-code" maxlength="6" pattern="[0-9]*"></label>
      <button class="btn primary block" type="submit" id="auth-submit">Sign in</button>
    </form>`, 'Sign in to your hub');

  document.getElementById('login-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = new FormData(event.target);
    const button = document.getElementById('auth-submit');
    button.disabled = true;
    button.innerHTML = '<span class="spinner"></span> Signing in...';
    try {
      await api('/auth/login', { method: 'POST', body: {
        username: form.get('username'), password: form.get('password'), totp: form.get('totp') || '',
      }, allow401: true });
      state.me = await api('/me');
      state.hubName = state.me.hub_name || state.hubName;
      renderShell('overview');
      await route();
    } catch (err) {
      renderAuth('login', err.message);
    }
  });
}

async function logout() {
  try { await api('/auth/logout', { method: 'POST' }); } catch { /* ignore */ }
  state.me = null;
  disconnectWS();
  renderAuth('login');
}

/* ---------- pages ---------- */

async function route() {
  if (!state.me) return;
  if (state.pageCleanup) {
    try { state.pageCleanup(); } catch { /* ignore */ }
    state.pageCleanup = null;
  }
  state.wsHandlers.clear();
  state.run = null;
  const hash = location.hash || '#/';
  try {
    if (hash.startsWith('#/hosts')) return await pageHosts();
    if (hash.startsWith('#/sessions/')) return await pageSession(hash.split('/')[2]);
    if (hash.startsWith('#/sessions')) return await pageSessions();
    if (hash.startsWith('#/runs/')) return await pageRun(hash.split('/')[2]);
    if (hash.startsWith('#/search')) return await pageSearch();
    if (hash.startsWith('#/devices')) return await pageDevices();
    if (hash.startsWith('#/audit')) return await pageAudit();
    return await pageOverview();
  } catch (err) {
    if (err.status === 401) return;
    const view = document.getElementById('view');
    if (view) view.innerHTML = `<div class="notice error">${esc(err.message)}</div>`;
  }
}

async function pageOverview() {
  renderShell('overview');
  const view = setPage('Overview', 'All of your coding agents, across every paired machine.', '<a class="btn small" href="#/sessions">All sessions</a>');
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const [{ hosts }, { sessions }] = await Promise.all([
    api('/hosts'),
    api('/sessions?limit=500'),
  ]);
  const recent = sessions.slice(0, 8);
  const online = hosts.filter((h) => h.status === 'online').length;
  const attention = sessions.filter((s) => s.state === 'waiting_for_input' || s.state === 'running');
  const agents = new Set();
  hosts.forEach((h) => (h.agents || []).forEach((a) => agents.add(a.name)));
  view.innerHTML = `
    <div class="grid cols-4">
      <div class="card stat"><span class="value">${hosts.filter((h) => h.status === 'online').length}<span class="muted" style="font-size:16px">/${hosts.length}</span></span><span class="label">Hosts online</span><span class="delta">${hosts.length ? 'connected to this hub' : 'pair your first machine'}</span></div>
      <div class="card stat"><span class="value">${sessions.length}</span><span class="label">Recent sessions</span><span class="delta">latest across all hosts</span></div>
      <div class="card stat"><span class="value">${attention.length}</span><span class="label">Need attention</span><span class="delta">running or waiting for input</span></div>
      <div class="card stat"><span class="value">${agents.size}</span><span class="label">Agent types</span><span class="delta">${[...agents].map(esc).join(' · ') || 'none detected yet'}</span></div>
    </div>

    <div class="grid cols-2 mt-3" style="align-items:start">
      <section>
        <div class="section-head"><h2>Recent sessions</h2><a class="small" href="#/sessions">View all</a></div>
        ${sessions.length ? `<div class="list">${recent.map(sessionRow).join('')}</div>` : `<div class="card empty"><strong>No sessions yet</strong>Pair a host and AgentNexus will discover existing agent sessions automatically.</div>`}
      </section>
      <section>
        <div class="section-head"><h2>Hosts</h2><a class="small" href="#/hosts">Manage</a></div>
        ${hosts.length ? `<div class="list">${hosts.map(hostRowCompact).join('')}</div>` : `<div class="card empty"><strong>No hosts</strong>Open Hosts to create a pairing code.</div>`}
      </section>
    </div>`;
}

function hostRowCompact(h) {
  return `<a class="list-row" href="#/hosts">
    <div><div class="title">${esc(h.name)}</div><div class="sub"><span>${esc(h.os)}/${esc(h.arch)}</span><span>${(h.agents || []).length} agent(s)</span></div></div>
    <div class="right">${badge(h.status === 'online' ? 'online' : 'offline', h.status)}</div>
  </a>`;
}

function sessionRow(s) {
  return `<a class="list-row" href="#/sessions/${esc(s.id)}">
    <div style="min-width:0">
      <div class="title">${esc(s.title || 'Untitled session')}</div>
      <div class="sub"><span>${esc(s.host_name)}</span><span>${esc(s.cwd || '')}</span><span>${fmtRelative(s.updated_at)}</span></div>
    </div>
    <div class="right">${agentBadge(s.agent)}${stateBadge(s.state)}</div>
  </a>`;
}

async function pageHosts() {
  renderShell('hosts');
  const view = setPage('Hosts', 'Machines running nexusd. Pairing is always initiated from this hub.', '<button class="btn primary small" id="pair-host">Pair a host</button>');
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const [{ hosts }] = await Promise.all([api('/hosts')]);
  document.getElementById('pair-host').addEventListener('click', pairHostDialog);
  state.wsHandlers.add((msg) => {
    if (msg.type === 'host.updated') pageHosts().catch(() => {});
    if (msg.type === 'sessions.changed') { /* no-op on hosts page */ }
  });
  if (!hosts.length) {
    view.innerHTML = `<div class="card empty"><strong>No hosts paired</strong>Create a one-time code, then run <code>nexusd pair --hub ${esc(location.origin)} --code &lt;code&gt;</code> on the machine.</div>`;
    return;
  }
  view.innerHTML = `<div class="grid cols-2">${hosts.map((h) => `
    <div class="card host-card">
      <div class="host-head">
        <div class="host-title">
          <div class="brand-mark" style="width:34px;height:34px;font-size:15px">${esc((h.name || '?').slice(0, 1).toUpperCase())}</div>
          <div><h3>${esc(h.name)}</h3><div class="meta">${esc(h.os)}/${esc(h.arch)} · nexusd ${esc(h.version || 'unknown')}</div></div>
        </div>
        ${badge(h.status === 'online' ? 'online' : 'offline', h.status)}
      </div>
      <div class="mono-small">id ${esc(h.id)}<br>last seen ${esc(fmtTime(h.last_seen))}</div>
      <div class="agent-chips">
        ${(h.agents || []).map((a) => `<span class="chip"><b>${esc(a.name)}</b> ${esc(a.version)}</span>`).join('') || '<span class="chip">no agents detected</span>'}
      </div>
      <div class="split wrap">
        <button class="btn small" data-new-session="${esc(h.id)}" ${h.status !== 'online' ? 'disabled' : ''}>New session</button>
        <button class="btn small danger" data-revoke-host="${esc(h.id)}">Revoke</button>
      </div>
    </div>`).join('')}</div>`;
  view.querySelectorAll('[data-new-session]').forEach((b) => b.addEventListener('click', () => newSessionDialog(hosts.find((h) => h.id === b.dataset.newSession))));
  view.querySelectorAll('[data-revoke-host]').forEach((b) => b.addEventListener('click', () => {
    confirmDanger('Revoking this host closes its connection and fails active runs. The daemon must be paired again.', async () => {
      await api(`/hosts/${b.dataset.revokeHost}`, { method: 'DELETE' });
      toast('Host revoked');
      pageHosts();
    });
  }));
}

function pairHostDialog() {
  modal(`
    <h2>Pair a host</h2>
    <p class="hint">The code is single-use and expires after 10 minutes. The host connects outbound; no inbound ports are required.</p>
    <div class="empty" id="pair-code"><span class="spinner"></span></div>
    <div id="pair-steps"></div>
    <div class="modal-actions"><button class="btn ghost" type="button" data-close>Close</button></div>`);
  api('/hosts/pairing-codes', { method: 'POST' }).then((data) => {
    const box = document.getElementById('pair-code');
    if (!box) return;
    box.className = 'secret';
    box.textContent = data.code;
    const origin = location.origin.replace(/^http/, 'ws');
    document.getElementById('pair-steps').innerHTML = `
      <p class="small muted mt-2">Run this on the machine you want to add:</p>
      <div class="secret">nexusd pair --hub ${esc(location.origin)} --code ${esc(data.code)}</div>
      <div class="split mt-1">
        <button class="btn small" id="copy-code">Copy code</button>
        <button class="btn small" id="copy-command">Copy command</button>
      </div>
      <p class="small muted mt-2">Then install the per-user service: <code>nexusd service install</code>. The host is accepted by this hub at <code>${esc(origin)}/v1/host/connect</code>.</p>`;
    document.getElementById('copy-code').addEventListener('click', () => copyText(data.code));
    document.getElementById('copy-command').addEventListener('click', () => copyText(`nexusd pair --hub ${location.origin} --code ${data.code}`));
  }).catch((err) => {
    const box = document.getElementById('pair-code');
    if (box) { box.className = 'notice error'; box.textContent = err.message; }
  });
}

async function newSessionDialog(host) {
  const agents = (host?.agents || []).filter((a) => a.supported);
  modal(`
    <h2>New session on ${esc(host?.name || 'host')}</h2>
    <p class="hint">The agent starts in a directory permitted by the host policy. Permission-bypass flags are rejected on the host.</p>
    <form class="form" id="new-session-form">
      <label class="label">Agent<select class="select" name="agent">${agents.map((a) => `<option value="${esc(a.name)}">${esc(a.name)} — ${esc(a.version)}</option>`).join('')}</select></label>
      <label class="label">Working directory<input class="input" name="cwd" placeholder="C:\\Users\\me\\project or /home/me/project" required></label>
      <div class="notice info">The host enforces its own <code>allowed_roots</code>; a path outside them is rejected even if this form accepts it.</div>
      <div class="modal-actions">
        <button class="btn ghost" type="button" data-close>Cancel</button>
        <button class="btn primary" type="submit">Start terminal</button>
      </div>
    </form>`);
  document.getElementById('new-session-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = new FormData(event.target);
    try {
      const run = await api(`/hosts/${host.id}/runs`, { method: 'POST', body: {
        agent: form.get('agent'), cwd: form.get('cwd'), mode: 'terminal', cols: 120, rows: 32,
      } });
      closeModal();
      location.hash = `#/runs/${run.id}`;
    } catch (err) {
      toast(err.message, true);
    }
  });
}

async function pageSessions() {
  renderShell('sessions');
  const params = new URLSearchParams(location.hash.split('?')[1] || '');
  const view = setPage('Sessions', 'Discovered natively from each host; nothing is copied or rewritten.');
  view.innerHTML = `
    <div class="toolbar">
      <input class="input grow" id="filter-q" placeholder="Filter title or project..." value="${esc(params.get('q') || '')}">
      <select class="select" id="filter-agent"><option value="">All agents</option>${['claude', 'codex', 'opencode', 'gemini'].map((a) => `<option ${params.get('agent') === a ? 'selected' : ''}>${a}</option>`).join('')}</select>
      <select class="select" id="filter-state"><option value="">All states</option>${['running', 'waiting_for_input', 'idle', 'completed', 'failed', 'unknown'].map((s) => `<option ${params.get('state') === s ? 'selected' : ''}>${s}</option>`).join('')}</select>
      <button class="btn" id="apply-filter">Apply</button>
    </div>
    <div id="session-list"><div class="empty"><span class="spinner"></span></div></div>`;
  const load = async () => {
    const q = new URLSearchParams();
    const query = document.getElementById('filter-q').value.trim();
    if (query) q.set('q', query);
    const agent = document.getElementById('filter-agent').value;
    if (agent) q.set('agent', agent);
    const stateFilter = document.getElementById('filter-state').value;
    if (stateFilter) q.set('state', stateFilter);
    q.set('limit', '500');
    const data = await api('/sessions?' + q.toString());
    const list = document.getElementById('session-list');
    if (!list) return;
    list.innerHTML = data.sessions.length
      ? `<div class="list">${data.sessions.map(sessionRow).join('')}</div>`
      : `<div class="card empty"><strong>No sessions match</strong>Try a different filter, or wait for a host to reconnect.</div>`;
    history.replaceState(null, '', '#/sessions' + (q.toString() ? '?' + q.toString() : ''));
  };
  document.getElementById('apply-filter').addEventListener('click', () => load().catch((e) => toast(e.message, true)));
  document.getElementById('filter-q').addEventListener('keydown', (e) => { if (e.key === 'Enter') load().catch((err) => toast(err.message, true)); });
  state.wsHandlers.add((msg) => { if (msg.type === 'sessions.changed') load().catch(() => {}); });
  await load();
}

async function pageSession(id) {
  renderShell('sessions');
  const view = setPage('Session', 'Read-only transcript fetched on demand from the host.', '<a class="btn small" href="#/sessions">Back</a>');
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const s = await api(`/sessions/${id}`);
  let running = false;
  setPage(s.title || 'Session', `${s.host_name} · ${s.agent} · ${s.cwd || 'unknown directory'}`, `
    <button class="btn small" id="resume-session">Resume terminal</button>
    ${s.agent === 'codex' || s.agent === 'claude' ? '<button class="btn small" id="fork-session">Fork</button>' : ''}
    <a class="btn small ghost" href="#/sessions">Back</a>`);
  document.getElementById('resume-session').addEventListener('click', () => startRun(s, false));
  document.getElementById('fork-session')?.addEventListener('click', () => startRun(s, true));
  view.innerHTML = `
    <div class="card mb-2">
      <div class="split wrap">
        ${agentBadge(s.agent)} ${stateBadge(s.state)} ${s.active_elsewhere ? badge('active elsewhere', 'waiting_for_input') : ''}
        <span class="mono-small">${esc(s.cwd || '')}</span>
        <span class="right small muted">created ${fmtTime(s.created_at)} · updated ${fmtTime(s.updated_at)} · ${s.message_count || 0} messages</span>
      </div>
      ${s.warning ? `<div class="notice mt-1">${esc(s.warning)}</div>` : ''}
    </div>
    <div id="transcript" class="transcript"><div class="empty"><span class="spinner"></span></div></div>
    <div class="load-more" id="load-more-wrap"></div>`;
  let offset = 0;
  const append = async () => {
    const box = document.getElementById('transcript');
    const tr = await api(`/sessions/${encodeURIComponent(id)}/transcript?offset=${offset}&limit=80`);
    if (offset === 0) box.innerHTML = '';
    box.insertAdjacentHTML('beforeend', tr.messages.length ? tr.messages.map(renderMessage).join('') : '<div class="empty"><strong>No readable messages</strong>The host could not parse this transcript.</div>');
    offset = tr.next || 0;
    document.getElementById('load-more-wrap').innerHTML = tr.next ? '<button class="btn" id="load-more">Load more</button>' : '';
    document.getElementById('load-more')?.addEventListener('click', () => append().catch((e) => toast(e.message, true)));
  };
  await append();
}

function renderMessage(m) {
  if (m.kind === 'tool_call' || m.kind === 'tool_result') {
    const title = m.kind === 'tool_call' ? (m.tool_name || 'tool call') : 'tool result';
    const input = m.tool_input || '';
    const output = m.tool_output || '';
    return `<div class="msg tool"><div class="role">${esc(m.role)}</div><details class="tool">
      <summary><strong>${esc(title)}</strong><span class="muted">${esc(input || output).slice(0, 140)}</span></summary>
      ${input ? `<pre>${esc(input)}</pre>` : ''}
      ${output ? `<pre>${esc(output)}</pre>` : ''}
    </details></div>`;
  }
  return `<div class="msg ${esc(m.role)}"><div class="role">${esc(m.role)}</div><div class="body">${esc(m.text || '')}</div></div>`;
}

async function startRun(session, fork) {
  try {
    const run = await api(`/sessions/${session.id}/runs`, { method: 'POST', body: {
      mode: 'terminal', fork: Boolean(fork), cols: 120, rows: 32,
    } });
    location.hash = `#/runs/${run.id}`;
  } catch (err) {
    toast(err.message, true);
  }
}

async function pageSearch() {
  renderShell('search');
  const params = new URLSearchParams(location.hash.split('?')[1] || '');
  const q = params.get('q') || '';
  const view = setPage('Search', 'Federated transcript search runs on each online host. Content is never indexed on the hub.');
  view.innerHTML = `
    <form class="toolbar" id="search-form">
      <input class="input grow" id="search-q" placeholder="Search all transcripts, e.g. rate limiter" value="${esc(q)}" autofocus>
      <button class="btn primary" type="submit">Search</button>
    </form>
    <div id="search-results">${q ? '<div class="empty"><span class="spinner"></span></div>' : '<div class="card empty"><strong>Search across hosts</strong>Results come from the agent stores; offline hosts are reported, not silently skipped.</div>'}</div>`;
  const run = async (query) => {
    if (query.length < 2) return;
    history.replaceState(null, '', `#/search?q=${encodeURIComponent(query)}`);
    const box = document.getElementById('search-results');
    box.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
    const data = await api('/search?q=' + encodeURIComponent(query));
    const skipped = Object.keys(data.not_searched || {});
    box.innerHTML = `
      ${skipped.length ? `<div class="notice info mb-2">Not searched: ${skipped.map(esc).join(', ')} (offline)</div>` : ''}
      ${data.results.length ? `<div class="list">${data.results.map((r) => `
        <div class="list-row" style="cursor:default">
          <div style="min-width:0"><div class="title">${esc(r.title || 'Untitled session')}</div><div class="sub"><span>${esc(r.snippet)}</span></div></div>
          <div class="right">${agentBadge(r.agent)}<span class="small muted">${fmtRelative(r.at)}</span></div>
        </div>`).join('')}</div>` : `<div class="card empty"><strong>No matches</strong>Try another term or check that hosts are online.</div>`}`;
  };
  document.getElementById('search-form').addEventListener('submit', (e) => {
    e.preventDefault();
    run(document.getElementById('search-q').value.trim()).catch((err) => toast(err.message, true));
  });
  if (q) run(q).catch((err) => toast(err.message, true));
}

async function pageDevices() {
  renderShell('devices');
  const view = setPage('Devices', 'Signed-in browsers. Revoke anything you no longer trust; revocation ends its session immediately.');
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const data = await api('/devices');
  view.innerHTML = `
    <div class="table-wrap">
      <table><thead><tr><th>Device</th><th>Created</th><th>Last seen</th><th></th></tr></thead><tbody>
      ${data.devices.map((d) => `<tr>
        <td><b>${esc(d.name)}</b><div class="mono-small">${esc(shortId(d.id))}</div></td>
        <td>${esc(fmtTime(d.created_at))}</td>
        <td>${esc(fmtTime(d.last_seen))}</td>
        <td class="nowrap">${d.id === data.current_device_id ? badge('this device', 'online') : `<button class="btn small danger" data-revoke-device="${esc(d.id)}">Revoke</button>`}</td>
      </tr>`).join('')}
      </tbody></table>
    </div>
    <div class="card mt-3">
      <h2 class="mt-0">Authenticator (TOTP)</h2>
      <p class="hint">${state.me.totp_enabled ? 'TOTP is enabled. A code is required at every login.' : 'TOTP is not enabled yet. Enrolling it adds a second factor to password login.'}</p>
      <div id="totp-area">
        ${state.me.totp_enabled ? badge('enabled', 'online') : '<button class="btn" id="totp-begin">Set up authenticator</button>'}
      </div>
    </div>
    <p class="small muted mt-2">Passkeys/WebAuthn are on the roadmap; this release uses Argon2id passwords and optional TOTP. Session cookies are HttpOnly, SameSite=Strict and Secure when TLS is configured.</p>`;
  view.querySelectorAll('[data-revoke-device]').forEach((b) => b.addEventListener('click', () => {
    confirmDanger('Revoking this device ends all of its signed-in sessions.', async () => {
      await api(`/devices/${b.dataset.revokeDevice}`, { method: 'DELETE' });
      toast('Device revoked');
      pageDevices();
    });
  }));
  document.getElementById('totp-begin')?.addEventListener('click', async () => {
    try {
      const data = await api('/auth/totp/begin', { method: 'POST' });
      modal(`
        <h2>Set up authenticator</h2>
        <p class="hint">Add this secret to your authenticator app, then enter the current six-digit code to confirm.</p>
        <div class="secret">${esc(data.secret)}</div>
        <div class="secret mt-1">${esc(data.url)}</div>
        <form class="form mt-2" id="totp-form">
          <label class="label">Current code<input class="input" name="code" inputmode="numeric" maxlength="6" pattern="[0-9]{6}" required></label>
          <div class="modal-actions"><button class="btn ghost" type="button" data-close>Cancel</button><button class="btn primary" type="submit">Enable TOTP</button></div>
        </form>`);
      document.getElementById('totp-form').addEventListener('submit', async (e) => {
        e.preventDefault();
        try {
          await api('/auth/totp/enable', { method: 'POST', body: { secret: data.secret, code: new FormData(e.target).get('code') } });
          closeModal();
          state.me = await api('/me');
          toast('TOTP enabled');
          pageDevices();
        } catch (err) { toast(err.message, true); }
      });
    } catch (err) { toast(err.message, true); }
  });
}

async function pageAudit() {
  renderShell('audit');
  const view = setPage('Audit log', 'Append-only, hash-chained security events stored on the hub.');
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const data = await api('/audit?limit=300');
  view.innerHTML = `
    ${data.broken_seq ? `<div class="notice error mb-2">Audit chain broken at sequence ${esc(data.broken_seq)}. Investigate the database immediately.</div>` : `<div class="notice info mb-2">Hash chain verified: no broken link in the checked range.</div>`}
    <div class="table-wrap"><table>
      <thead><tr><th>Seq</th><th>Time</th><th>Actor</th><th>Action</th><th>Details</th></tr></thead>
      <tbody>${data.events.map((e) => `<tr>
        <td class="mono">${esc(e.seq)}</td><td class="nowrap">${esc(fmtTime(e.at))}</td>
        <td>${esc(e.actor)}</td><td class="mono">${esc(e.action)}</td>
        <td class="mono" style="max-width:420px;white-space:pre-wrap;word-break:break-all">${esc(e.details)}</td>
      </tr>`).join('')}</tbody>
    </table></div>`;
}

async function pageRun(id) {
  renderShell('sessions');
  const run = await api(`/runs/${id}`);
  state.run = run;
  setPage('Terminal', `run ${shortId(run.id)} · ${run.mode} mode`, `<button class="btn small danger" id="stop-run">Stop run</button><a class="btn small ghost" href="#/sessions">Sessions</a>`);
  const view = document.getElementById('view');
  view.innerHTML = `
    <div class="card mb-2">
      <div class="run-head">
        <div class="split wrap">${stateBadge(run.state)}<span class="mono-small">host ${esc(shortId(run.host_id))}</span><span class="mono-small">lease ${esc(shortId(run.lease_device_id || ''))}</span></div>
        <div class="small muted" id="run-note">Connecting...</div>
      </div>
      <div class="notice info small">Resuming the agent's own TUI in a PTY. The session remains native; typing is only accepted from the controller lease.</div>
    </div>
    <div class="terminal-shell">
      <div class="terminal-bar"><span class="dot" id="term-dot"></span><span id="term-status">disconnected</span><span class="right mono-small">${esc(run.id)}</span></div>
      <div class="terminal" id="terminal"></div>
      <div class="keybar">
        <button class="btn small" data-key="esc">Esc</button>
        <button class="btn small" data-key="tab">Tab</button>
        <button class="btn small" data-key="ctrl-c">Ctrl-C</button>
        <button class="btn small" data-key="up">↑</button>
        <button class="btn small" data-key="down">↓</button>
        <button class="btn small" data-key="left">←</button>
        <button class="btn small" data-key="right">→</button>
        <button class="btn small" data-key="enter">Enter</button>
      </div>
    </div>`;
  const term = new Terminal({
    cursorBlink: true,
    fontFamily: 'Cascadia Code, SFMono-Regular, Consolas, monospace',
    fontSize: 13,
    lineHeight: 1.2,
    theme: {
      background: '#05080b', foreground: '#dbe7f3', cursor: '#5eead4', selectionBackground: '#1e4a45',
      black: '#0b0f14', red: '#fb7185', green: '#4ade80', yellow: '#fbbf24', blue: '#7dd3fc',
      magenta: '#c4b5fd', cyan: '#5eead4', white: '#e8eef5',
    },
    scrollback: 10000,
    allowProposedApi: false,
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(document.getElementById('terminal'));
  fit.fit();
  const dot = document.getElementById('term-dot');
  const status = document.getElementById('term-status');
  const note = document.getElementById('run-note');
  const setLive = (text) => { status.textContent = text; dot.classList.toggle('live', text === 'connected'); };
  let lastSeq = 0;
  let subscribedGen = 0;
  const input = (data) => {
    if (run.lease_device_id && state.me.device.id && run.lease_device_id !== state.me.device.id) {
      note.textContent = 'Read-only: another device holds the lease.';
      return;
    }
    wsSend({ type: 'input', run_id: id, data: btoa(String.fromCharCode(...new TextEncoder().encode(data))) });
  };
  term.onData(input);
  const resize = () => {
    fit.fit();
    wsSend({ type: 'resize', run_id: id, cols: term.cols, rows: term.rows });
  };
  const onResize = () => resize();
  window.addEventListener('resize', onResize);
  const ro = new ResizeObserver(() => resize());
  ro.observe(document.getElementById('terminal'));

  api(`/runs/${id}/lease`, { method: 'POST' }).then(() => {
    run.lease_device_id = state.me.device.id;
    note.textContent = 'You have control (controller lease).';
  }).catch((err) => { note.textContent = 'Read-only: ' + err.message; });

  document.getElementById('stop-run').addEventListener('click', async () => {
    try { await api(`/runs/${id}/stop`, { method: 'POST' }); toast('Stopping run'); }
    catch (err) { toast(err.message, true); }
  });

  const sendSubscribe = () => {
    if (wsSend({ type: 'subscribe', run_id: id, last_seq: lastSeq })) subscribedGen = state.wsGen;
  };
  const handler = (msg) => {
    if (msg.run_id !== id && msg.type !== 'ready') return;
    switch (msg.type) {
      case 'run.output':
        if (msg.data) {
          term.write(b64decode(msg.data));
          lastSeq = Math.max(lastSeq, msg.seq || 0);
        }
        break;
      case 'run.state':
        if (msg.state) { run.state = msg.state; note.textContent = `state: ${msg.state}`; }
        break;
      case 'run.exit':
        run.state = msg.state || 'exited';
        setLive('exited');
        note.textContent = msg.message || 'agent exited';
        term.write(`\r\n\x1b[2m[run ${run.state}]\x1b[0m\r\n`);
        break;
      case 'run.error':
        note.textContent = msg.error || 'run failed';
        term.write(`\r\n\x1b[31m[${msg.error || 'run failed'}]\x1b[0m\r\n`);
        break;
      case 'run.lease':
        note.textContent = 'Lease transferred to ' + (msg.message || 'another device');
        break;
    }
  };
  state.wsHandlers.add(handler);
  // The websocket may still be connecting, and it can reconnect later. Keep
  // re-subscribing on each new socket generation so buffered output is
  // replayed from lastSeq (FR-27).
  sendSubscribe();
  const timer = setInterval(() => {
    if (!state.me) { clearInterval(timer); return; }
    if (state.ws?.readyState === WebSocket.OPEN) {
      if (subscribedGen !== state.wsGen) sendSubscribe();
      setLive('connected');
    } else {
      setLive('reconnecting');
    }
  }, 500);
  state.pageCleanup = () => {
    clearInterval(timer);
    state.wsHandlers.delete(handler);
    window.removeEventListener('resize', onResize);
    ro.disconnect();
    term.dispose();
  };
  document.querySelectorAll('[data-key]').forEach((b) => b.addEventListener('click', () => {
    const keys = { esc: '\x1b', tab: '\t', 'ctrl-c': '\x03', up: '\x1b[A', down: '\x1b[B', left: '\x1b[D', right: '\x1b[C', enter: '\r' };
    input(keys[b.dataset.key] || '');
    term.focus();
  }));
  term.focus();
}

/* ---------- boot ---------- */

async function boot() {
  try {
    const status = await api('/setup/status', { allow401: true });
    state.setupRequired = status.setup_required;
    state.hubName = status.hub_name || state.hubName;
    if (status.setup_required) return renderAuth('setup');
    try {
      state.me = await api('/me', { allow401: true });
      renderShell('overview');
      await route();
    } catch {
      renderAuth('login');
    }
  } catch (err) {
    app.innerHTML = `<div class="auth-wrap"><div class="card"><h2>AgentNexus is unreachable</h2><p class="hint">${esc(err.message)}</p></div></div>`;
  }
}

window.addEventListener('hashchange', () => route());
boot();
