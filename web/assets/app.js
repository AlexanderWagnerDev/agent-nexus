/* AgentNexus browser client — dependency-free ES modules. */
import { renderMarkdown, highlightCode } from './markdown.js';
import { describeTool } from './chat-format.js';
import { parseUnifiedDiff, renderUnifiedDiff, renderSplitDiff, diffFileList, diffSummary } from './diff.js';
import { t, tn, initLang, getLang, setLang } from './i18n.js';

initLang();

const app = document.getElementById('app');
const modalRoot = document.getElementById('modal-root');
const toastRoot = document.getElementById('toast-root');

const iconPaths = {
  home: '<path d="M3 10.5 12 3l9 7.5"/><path d="M5 9.5V21h14V9.5"/><path d="M9 21v-6h6v6"/>',
  overview: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>',
  hosts: '<rect x="3" y="4" width="18" height="12" rx="1.5"/><path d="M8 20h8M12 16v4"/>',
  projects: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>',
  sessions: '<path d="M4 5h16v11H9l-5 4V5Z"/>',
  runs: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 9 3 3-3 3M13 16h4"/>',
  search: '<circle cx="11" cy="11" r="6"/><path d="m16 16 4 4"/>',
  settings: '<path d="M4 7h8M17 7h3M4 17h3M12 17h8M4 12h12M19 12h1"/><circle cx="15" cy="7" r="2"/><circle cx="9" cy="17" r="2"/><circle cx="18" cy="12" r="2"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  moon: '<path d="M20 14.5A8 8 0 0 1 9.5 4 7 7 0 1 0 20 14.5Z"/>',
  monitor: '<rect x="3" y="4" width="18" height="12" rx="1.5"/><path d="M8 20h8M12 16v4"/>',
  command: '<path d="M9 6a3 3 0 1 0-3 3h12a3 3 0 1 0-3-3v12a3 3 0 1 0 3-3H6a3 3 0 1 0 3 3V6Z"/>',
  bell: '<path d="M6 9a6 6 0 1 1 12 0c0 5 3 6 3 6H3s3-1 3-6"/><path d="M10 20a2 2 0 0 0 4 0"/>',
  panelLeft: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>',
  panelRight: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/>',
  star: '<path d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1 6.2-5.5-3-5.5 3 1-6.2L3 9.6l6.2-.9Z"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>',
  branch: '<circle cx="6" cy="5" r="2"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="7" r="2"/><path d="M6 7v10M8 7h4a6 6 0 0 0 6-6"/><path d="M18 9a6 6 0 0 1-6 6H8"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  check: '<path d="m4 12 5 5L20 6"/>',
  x: '<path d="m5 5 14 14M19 5 5 19"/>',
  alert: '<path d="M12 3 2.5 20h19Z"/><path d="M12 9v5M12 17h.01"/>',
  chevronDown: '<path d="m6 9 6 6 6-6"/>',
  chevronRight: '<path d="m9 6 6 6-6 6"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  send: '<path d="m4 12 16-8-6 16-3-6Z"/>',
  stop: '<rect x="6" y="6" width="12" height="12" rx="2"/>',
  copy: '<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/>',
  external: '<path d="M14 4h6v6M20 4l-9 9"/><path d="M20 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1h5"/>',
  terminal: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 9 3 3-3 3M13 16h4"/>',
  file: '<path d="M6 3h8l4 4v14H6Z"/><path d="M14 3v5h5"/>',
  edit: '<path d="M4 20h4L19 9l-4-4L4 16Z"/><path d="m13 7 4 4"/>',
  searchFile: '<path d="M6 3h8l4 4v6"/><path d="M14 3v5h5"/><circle cx="11" cy="16" r="4"/><path d="m14 19 4 4"/>',
  brain: '<path d="M9 4a3 3 0 0 0-3 3 3 3 0 0 0-1 5.8V16a3 3 0 0 0 4 2.8V20h3V4Z"/><path d="M15 4a3 3 0 0 1 3 3 3 3 0 0 1 1 5.8V16a3 3 0 0 1-4 2.8V20h-3"/>',
  tool: '<path d="M14.5 6.5a4 4 0 0 0-5.6 4.7L4 16.1V20h3.9l4.9-4.9a4 4 0 0 0 4.7-5.6l-2.5 2.5-2.1-2.1Z"/>',
  bot: '<rect x="4" y="7" width="16" height="12" rx="3"/><path d="M12 3v4M8 12h.01M16 12h.01M8 16h8"/>',
  menu: '<path d="M4 6h16M4 12h16M4 18h16"/>',
  refresh: '<path d="M20 11a8 8 0 1 0-2 5.3"/><path d="M20 4v7h-7"/>',
  arrowDown: '<path d="M12 4v16M6 14l6 6 6-6"/>',
  git: '<circle cx="6" cy="5" r="2"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="7" r="2"/><path d="M6 7v10M8 7h4a6 6 0 0 0 6-6M18 9a6 6 0 0 1-6 6H8"/>',
  image: '<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9" r="1.5"/><path d="m21 16-5-5-8 8"/>',
  web: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a15 15 0 0 1 0 18 15 15 0 0 1 0-18"/>',
  paperclip: '<path d="m20 11-7.5 7.5a5 5 0 0 1-7-7L13 4a3.5 3.5 0 0 1 5 5l-7.5 7.5a2 2 0 0 1-3-3L14 7"/>',
  task: '<path d="M9 6h11M9 12h11M9 18h11"/><path d="m3 6 1 1 2-2M3 12l1 1 2-2M3 18l1 1 2-2"/>',
  plan: '<path d="M8 4h8v4H8Z"/><path d="M6 6H4v15h16V6h-2"/><path d="M8 12h8M8 16h5"/>',
  mcp: '<path d="M8 12a4 4 0 0 1 4-4h4a4 4 0 0 1 0 8h-2"/><path d="M16 12a4 4 0 0 1-4 4H8a4 4 0 0 1 0-8h2"/>',
};

function svgIcon(name, size = 16) {
  return `<svg viewBox="0 0 24 24" width="${size}" height="${size}" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${iconPaths[name] || iconPaths.overview}</svg>`;
}

const state = {
  me: null,
  setupRequired: false,
  hubName: 'AgentNexus',
  version: '',
  embed: false,
  settings: { theme: 'system' },
  ws: null,
  wsRetry: 1000,
  wsGen: 0,
  wsHandlers: new Set(),
  pageCleanup: null,
  run: null,
  favorites: new Set(),
  notifications: [],
  unread: 0,
  notifFilter: 'all',
  openTabs: [],
  palette: null,
  notifTimer: null,
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

function uiLocale() {
  return getLang() === 'de' ? 'de-DE' : 'en-US';
}

function fmtTime(value) {
  if (!value) return '—';
  const d = new Date(value);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1970) return '—';
  return d.toLocaleString(uiLocale(), { dateStyle: 'medium', timeStyle: 'short' });
}

function fmtStamp(value) {
  if (!value) return '';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '';
  const now = new Date();
  const time = d.toLocaleTimeString(uiLocale(), { hour: '2-digit', minute: '2-digit' });
  if (d.toDateString() === now.toDateString()) return time;
  return `${d.toLocaleDateString(uiLocale(), { month: 'short', day: 'numeric' })}, ${time}`;
}

function fmtRelative(value) {
  if (!value) return '—';
  const d = new Date(value);
  const diff = Date.now() - d.getTime();
  if (Number.isNaN(diff)) return '—';
  const s = Math.round(diff / 1000);
  if (s < 45) return t('just now');
  const m = Math.round(s / 60);
  if (m < 60) return `${m}${t('m ago')}`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}${t('h ago')}`;
  const days = Math.round(h / 24);
  if (days < 30) return `${days}${t('d ago')}`;
  return new Date(value).toLocaleDateString(uiLocale());
}

function shortId(id) {
  return id ? id.slice(0, 10) : '';
}

function countLabel(count, singular, plural) {
  return `${count} ${t(count === 1 ? singular : plural)}`;
}

const AGENTS = {
  opencode: { label: 'OpenCode', short: 'OC' },
  codex: { label: 'Codex', short: 'CX' },
  claude: { label: 'Claude Code', short: 'CL' },
  gemini: { label: 'Gemini', short: 'GM' },
  copilot: { label: 'Copilot', short: 'CP' },
  goose: { label: 'Goose', short: 'GO' },
};

function agentMeta(agent) {
  return AGENTS[String(agent || '').toLowerCase()] || { label: agent || 'Agent', short: String(agent || '?').slice(0, 2).toUpperCase() };
}

function agentVersion(name, version) {
  const value = String(version || '').trim();
  if (!value) return '';
  return value.replace(new RegExp(`^${String(name || '').replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}[\\s-]*(cli[\\s-]*)?`, 'i'), '') || value;
}

function projectName(cwd) {
  const clean = String(cwd || '').replaceAll('\\', '/').replace(/\/+$/, '');
  if (!clean) return 'No project';
  const parts = clean.split('/');
  return parts[parts.length - 1] || clean;
}

function cwdKey(cwd) {
  return String(cwd || '').replaceAll('\\', '/').replace(/\/+$/, '').toLowerCase();
}

function repoLink(url, label = '') {
  const value = String(url || '');
  if (!/^https?:\/\//i.test(value)) return '';
  const name = label || value.replace(/^https?:\/\/[^/]+\//, '');
  return `<a class="repo-link" href="${esc(value)}" target="_blank" rel="noopener noreferrer">${svgIcon('git', 13)}${esc(name)}</a>`;
}

function fmtDuration(value) {
  if (!value) return '—';
  const ms = typeof value === 'number' ? value : Date.now() - new Date(value).getTime();
  if (!Number.isFinite(ms) || ms < 0) return '—';
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}

function storeGet(key, fallback) {
  try { return JSON.parse(localStorage.getItem(key)) ?? fallback; } catch { return fallback; }
}

function storeSet(key, value) {
  try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* private mode */ }
}

function draftKey(runID) {
  return `nexus-draft-${runID}`;
}

function agentGlyph(agent, size = 'normal') {
  const meta = agentMeta(agent);
  return `<span class="agent-glyph ${size}" data-agent="${esc(String(agent || 'agent').toLowerCase())}" title="${esc(meta.label)}">${esc(meta.short)}</span>`;
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
  return badge(t((s || 'unknown').replaceAll('_', ' ')), map[s] || 'unknown');
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
  const { method = 'GET', body, allow401 = false, stepUpRetry = false } = options;
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
    if (res.status === 403 && data.step_up_required && !stepUpRetry) {
      await ensureStepUp();
      return api(path, { ...options, stepUpRetry: true });
    }
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

let modalOnClose = null;
function modal(html, onClose) {
  if (modalRoot.firstElementChild) closeModal();
  modalOnClose = onClose || null;
  modalRoot.innerHTML = `<div class="modal-backdrop" data-modal-backdrop><div class="modal" role="dialog" aria-modal="true">${html}</div></div>`;
  const backdrop = modalRoot.firstElementChild;
  backdrop.addEventListener('click', (e) => {
    if (e.target === backdrop) closeModal();
  });
  modalRoot.querySelectorAll('[data-close]').forEach((b) => b.addEventListener('click', closeModal));
}

function closeModal() {
  const cb = modalOnClose;
  modalOnClose = null;
  modalRoot.innerHTML = '';
  if (cb) cb();
}

function confirmDanger(message, onConfirm) {
  modal(`
    <h2>Are you sure?</h2>
    <p class="hint">${esc(message)}</p>
    <div class="modal-actions">
      <button class="btn ghost" type="button" data-close>${t('Cancel')}</button>
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
    const base = Math.min(state.wsRetry, 15000);
    state.wsRetry = Math.min(state.wsRetry * 1.7, 15000);
    // Jitter avoids a thundering herd when a hub restarts and every open
    // browser reconnects at once.
    setTimeout(connectWS, Math.round(base * (0.8 + Math.random() * 0.4)));
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
  ['#/', 'Home', 'home', 'overview'],
  ['#/projects', 'Projects', 'projects', 'projects'],
  ['#/sessions', 'Sessions', 'sessions', 'sessions'],
  ['#/runs', 'Active', 'runs', 'runs'],
  ['#/hosts', 'Hosts', 'hosts', 'hosts'],
  ['#/settings', 'Settings', 'settings', 'settings'],
];

function renderShell(active = 'overview') {
  const initials = (state.me?.username || '?').slice(0, 1).toUpperCase();
  const role = state.me?.role || 'member';
  const collapsed = storeGet('nexus-sidebar-collapsed', false);
  app.className = '';
  app.innerHTML = `
    <div class="layout ${collapsed ? 'sidebar-collapsed' : ''} ${state.embed ? 'embed' : ''}" id="layout">
      <aside class="sidebar" id="sidebar">
        <div class="sidebar-top">
          <a class="brand" href="#/" title="AgentNexus home">
            <span class="brand-mark">A</span>
            <span class="brand-text"><strong>AgentNexus</strong><small>${esc(state.hubName)}</small></span>
          </a>
          <button class="icon-btn sidebar-toggle" id="sidebar-toggle" type="button" title="${collapsed ? t('Expand sidebar') : t('Collapse sidebar')}" aria-label="${collapsed ? t('Expand sidebar') : t('Collapse sidebar')}">${svgIcon('panelLeft')}</button>
        </div>
        <nav class="nav" aria-label="${t('Main navigation')}">
          ${navItems.map(([href, label, icon, key]) => `
            <a href="${href}" class="${key === active ? 'active' : ''}" title="${t(label)}">
              <span class="icon" aria-hidden="true">${svgIcon(icon)}</span><span class="nav-label">${t(label)}</span>
            </a>`).join('')}
        </nav>
        <div class="sidebar-extra" id="sidebar-extra"><div class="side-loading"><span class="spinner"></span></div></div>
        <div class="sidebar-foot">
          <div class="user-chip" title="${esc(state.me.username)}">
            <span class="avatar">${esc(initials)}</span>
            <span class="user-meta nav-label"><strong>${esc(state.me.username)}</strong><small>${esc(t(role))}</small></span>
            <button class="icon-btn user-logout" id="logout" type="button" title="${t('Sign out')}" aria-label="${t('Sign out')}">${svgIcon('external')}</button>
          </div>
        </div>
      </aside>
      <main class="main">
        <header class="topbar">
          <div class="topbar-title">
            <button class="icon-btn mobile-nav" id="mobile-nav" type="button" aria-label="${t('Open navigation')}">${svgIcon('menu')}</button>
            <div><h1 id="page-title">${t('Home')}</h1><div class="sub" id="page-sub"></div></div>
          </div>
          <div class="topbar-actions" id="page-actions"></div>
          <div class="topbar-tools">
            <button class="command-trigger" id="command-trigger" type="button" aria-label="${t('Search or jump to')}">
              ${svgIcon('search')}<span>${t('Search or jump to…')}</span><kbd>Ctrl K</kbd>
            </button>
            <button class="icon-btn" id="theme-toggle" type="button" title="${t('Switch theme')}" aria-label="${t('Switch theme')}">${themeIcon()}</button>
            <button class="icon-btn notification-button" id="notif-toggle" type="button" title="${t('Notifications')}" aria-label="${t('Notifications')}">
              ${svgIcon('bell')}<span class="notification-badge" id="notif-badge"></span>
            </button>
          </div>
        </header>
        <div class="tabs-bar" id="tabs-bar"></div>
        <div class="view" id="view"></div>
        <div class="notif-popover" id="notif-panel" hidden></div>
      </main>
    </div>`;
  document.getElementById('logout').addEventListener('click', logout);
  document.getElementById('mobile-nav')?.addEventListener('click', () => document.getElementById('layout').classList.toggle('mobile-sidebar-open'));
  document.getElementById('sidebar-toggle')?.addEventListener('click', toggleSidebar);
  document.getElementById('command-trigger')?.addEventListener('click', openPalette);
  document.getElementById('notif-toggle')?.addEventListener('click', toggleNotifications);
  document.getElementById('theme-toggle')?.addEventListener('click', async () => {
    const order = ['system', 'dark', 'light'];
    const current = state.settings.theme || 'system';
    const next = order[(order.indexOf(current) + 1) % order.length];
    try {
      await saveTheme(next);
      toast(`Theme: ${next}`);
    } catch (err) {
      toast(err.message, true);
    }
  });
  if (!state.embed) {
    loadSidebar().catch(() => {});
    loadNotifications().catch(() => {});
  }
  let sidebarTimer = null;
  state.wsHandlers.add((msg) => {
    if (msg.type === 'host.updated' || msg.type === 'sessions.changed' || msg.type === 'run.state' || msg.type === 'run.exit') {
      clearTimeout(sidebarTimer);
      sidebarTimer = setTimeout(() => loadSidebar().catch(() => {}), 700);
    }
    if (msg.type === 'run.exit' || msg.type === 'run.error' || (msg.type === 'run.event' && msg.event?.type === 'approval.requested')) {
      setTimeout(() => loadNotifications().catch(() => {}), 500);
    }
  });
  renderTabs();
  if (state.notifTimer) clearInterval(state.notifTimer);
  if (!state.embed) state.notifTimer = setInterval(() => { if (state.me) loadNotifications().catch(() => {}); }, 30000);
  connectWS();
}

function themeIcon() {
  const mode = state.settings.theme || 'system';
  return svgIcon(mode === 'dark' ? 'moon' : mode === 'light' ? 'sun' : 'monitor');
}

function toggleSidebar() {
  const layout = document.getElementById('layout');
  if (!layout) return;
  const collapsed = layout.classList.toggle('sidebar-collapsed');
  storeSet('nexus-sidebar-collapsed', collapsed);
  const button = document.getElementById('sidebar-toggle');
  if (button) {
    const label = collapsed ? 'Expand sidebar' : 'Collapse sidebar';
    button.title = label;
    button.setAttribute('aria-label', label);
  }
}

function sidebarSession(s) {
  const title = s.title || t('Untitled session');
  return `<a class="side-row side-session" href="#/sessions/${esc(s.id)}" title="${esc(title)}">
    <span class="side-state ${s.state === 'running' ? 'running' : s.state === 'waiting_for_input' ? 'waiting' : ''}"></span>
    ${agentGlyph(s.agent, 'small')}
    <span class="truncate">${esc(title)}</span>
  </a>`;
}

async function loadSidebar() {
  const box = document.getElementById('sidebar-extra');
  if (!box) return;
  try {
    const [{ hosts }, { sessions, has_more }, { projects }, favorites] = await Promise.all([
      api('/hosts'),
      api('/sessions?limit=200'),
      api('/projects?limit=40'),
      api('/favorites'),
    ]);
    state.favorites = new Set(Object.keys(favorites.ids || {}));
    const active = sessions.filter((s) => s.state === 'running' || s.state === 'waiting_for_input').slice(0, 5);
    const recent = sessions.slice(0, 6);
    const pinned = favorites.sessions || [];
    const projectGroups = (projects || []).slice(0, 6).map((p) => {
      const items = sessions.filter((s) => s.host_id === p.host_id && cwdKey(s.cwd) === cwdKey(p.cwd)).slice(0, 3);
      return `<details class="side-project" ${p.active ? 'open' : ''}>
        <summary title="${esc(p.cwd)}">
          <span class="side-project-name">${svgIcon('folder')}<span class="truncate">${esc(p.name)}</span></span>
          ${p.active ? `<span class="count active">${p.active}</span>` : `<span class="count">${p.sessions}</span>`}
          ${svgIcon('chevronRight')}
        </summary>
        <div class="side-project-meta">${esc(p.host_name)}${p.branch ? ` · ${esc(p.branch)}` : ''}</div>
        ${items.map(sidebarSession).join('')}
        <a class="side-more" href="#/sessions?project=${encodeURIComponent(p.cwd)}&host=${encodeURIComponent(p.host_id)}">${t('All')} ${countLabel(p.sessions, 'session', 'sessions')}</a>
      </details>`;
    }).join('');
    const hostRows = hosts.slice(0, 5).map((h) => `<a class="side-row" href="#/hosts" title="${esc(h.name)}"><span class="side-state ${h.status === 'online' ? 'online' : ''}"></span><span class="truncate">${esc(h.name)}</span><span class="side-meta">${(h.agents || []).length}</span></a>`).join('');
    box.innerHTML = `
      ${active.length ? `<div class="side-section"><div class="side-head"><span>${t('Active now')}</span><span class="side-count">${active.length}</span></div>${active.map(sidebarSession).join('')}</div>` : ''}
      ${pinned.length ? `<div class="side-section"><div class="side-head"><span>${t('Pinned')}</span></div>${pinned.slice(0, 5).map(sidebarSession).join('')}</div>` : ''}
      <div class="side-section"><div class="side-head"><span>${t('Projects')}</span><a href="#/projects">${t('All')}</a></div>${projectGroups || `<div class="side-empty">${t('No projects discovered yet')}</div>`}</div>
      <div class="side-section"><div class="side-head"><span>${t('Recent')}</span>${has_more ? `<span class="side-count">${t('Latest')}</span>` : ''}</div>${recent.map(sidebarSession).join('') || `<div class="side-empty">${t('No sessions yet')}</div>`}</div>
      <div class="side-section"><div class="side-head"><span>${t('Hosts')}</span><a href="#/hosts">${t('Manage')}</a></div>${hostRows || `<div class="side-empty">${t('No hosts paired')}</div>`}</div>`;
  } catch {
    box.innerHTML = `<div class="side-empty">${t('Could not load workspace.')}</div>`;
  }
}

async function saveTheme(theme) {
  const saved = await api('/me/settings', { method: 'PUT', body: { theme } });
  state.settings = { ...state.settings, theme: saved.theme };
  window.NexusTheme?.apply(saved.theme);
  const button = document.getElementById('theme-toggle');
  if (button) {
    button.innerHTML = themeIcon();
    button.title = `Theme: ${saved.theme}`;
  }
}

/* ---------- notifications ---------- */

function notificationIcon(kind) {
  return svgIcon({ approval: 'alert', failed: 'alert', disconnected: 'hosts', completed: 'check' }[kind] || 'bell');
}

async function loadNotifications() {
  const data = await api('/notifications?limit=40');
  state.notifications = data.notifications || [];
  state.unread = data.unread || 0;
  updateNotificationBadge();
  const panel = document.getElementById('notif-panel');
  if (panel && !panel.hidden) renderNotificationPanel();
}

function updateNotificationBadge() {
  const badgeNode = document.getElementById('notif-badge');
  if (!badgeNode) return;
  badgeNode.textContent = state.unread > 99 ? '99+' : String(state.unread || '');
  badgeNode.classList.toggle('visible', state.unread > 0);
  const toggle = document.getElementById('notif-toggle');
  toggle?.classList.toggle('has-unread', state.unread > 0);
  toggle?.setAttribute('aria-label', state.unread ? t('Notifications, {n} unread', { n: state.unread }) : t('Notifications'));
}

function renderNotificationPanel() {
  const panel = document.getElementById('notif-panel');
  if (!panel) return;
  const notifHost = storeGet('nexus-notif-host', '');
  const notifAgent = storeGet('nexus-notif-agent', '');
  const hosts = [...new Map(state.notifications.filter((n) => n.host_id).map((n) => [n.host_id, n.host_name || n.host_id])).entries()];
  const agents = [...new Set(state.notifications.filter((n) => n.agent).map((n) => n.agent))].sort();
  const items = state.notifications.filter((n) => {
    if (state.notifFilter === 'unread' && n.read) return false;
    if (notifHost && n.host_id !== notifHost) return false;
    if (notifAgent && n.agent !== notifAgent) return false;
    return true;
  });
  panel.innerHTML = `
    <div class="notif-head"><strong>${t('Notifications')}</strong><button class="btn ghost small" id="notif-read-all" ${state.unread ? '' : 'disabled'}>${t('Mark all read')}</button></div>
    <div class="notif-filter segmented" role="group" aria-label="${t('Filter notifications')}">
      <button class="segment ${state.notifFilter === 'all' ? 'active' : ''}" data-notif-filter="all" type="button">${t('All')}</button>
      <button class="segment ${state.notifFilter === 'unread' ? 'active' : ''}" data-notif-filter="unread" type="button">${t('Unread')}${state.unread ? ` (${state.unread})` : ''}</button>
    </div>
    ${hosts.length || agents.length ? `<div class="notif-scope">
      ${hosts.length > 1 ? `<select id="notif-host" aria-label="${t('Filter by host')}"><option value="">${t('All hosts')}</option>${hosts.map(([value, name]) => `<option value="${esc(value)}" ${value === notifHost ? 'selected' : ''}>${esc(name)}</option>`).join('')}</select>` : ''}
      ${agents.length > 1 ? `<select id="notif-agent" aria-label="${t('Filter by agent')}"><option value="">${t('All agents')}</option>${agents.map((value) => `<option value="${esc(value)}" ${value === notifAgent ? 'selected' : ''}>${esc(value)}</option>`).join('')}</select>` : ''}
    </div>` : ''}
    <div class="notif-list">${items.length ? items.map((n) => `
      <a class="notif-item ${n.read ? '' : 'unread'}" href="${esc(n.link || '#/')}" data-notif-id="${esc(n.id)}">
        <span class="notif-icon ${esc(n.kind)}">${notificationIcon(n.kind)}</span>
        <span class="notif-copy"><strong>${esc(n.title)}</strong><small>${esc(n.body)}</small><time>${esc(fmtRelative(n.created_at))}</time></span>
      </a>`).join('') : `<div class="notif-empty">${state.notifications.length && (state.notifFilter === 'unread' || notifHost || notifAgent) ? t('No notifications match these filters.') : t('No notifications yet. Run completions, failures and approvals will appear here.')}</div>`}</div>
    <div class="notif-foot"><a href="#/settings">${t('Notification settings')}</a><span>${state.unread} ${t('unread')}</span></div>`;
  panel.querySelectorAll('[data-notif-filter]').forEach((button) => button.addEventListener('click', () => {
    state.notifFilter = button.dataset.notifFilter;
    renderNotificationPanel();
  }));
  document.getElementById('notif-host')?.addEventListener('change', (event) => {
    storeSet('nexus-notif-host', event.target.value);
    renderNotificationPanel();
  });
  document.getElementById('notif-agent')?.addEventListener('change', (event) => {
    storeSet('nexus-notif-agent', event.target.value);
    renderNotificationPanel();
  });
  document.getElementById('notif-read-all')?.addEventListener('click', async () => {
    const data = await api('/notifications/read', { method: 'POST', body: {} });
    state.unread = data.unread;
    state.notifications = state.notifications.map((n) => ({ ...n, read: true }));
    updateNotificationBadge();
    renderNotificationPanel();
  });
  panel.querySelectorAll('[data-notif-id]').forEach((link) => link.addEventListener('click', async () => {
    const id = link.dataset.notifId;
    const target = state.notifications.find((n) => n.id === id);
    state.notifications = state.notifications.map((n) => n.id === id ? { ...n, read: true } : n);
    if (target && !target.read) state.unread = Math.max(0, state.unread - 1);
    updateNotificationBadge();
    api('/notifications/read', { method: 'POST', body: { id } }).catch(() => {});
    panel.hidden = true;
  }));
}

async function toggleNotifications() {
  const panel = document.getElementById('notif-panel');
  if (!panel) return;
  if (!panel.hidden) { panel.hidden = true; return; }
  panel.hidden = false;
  renderNotificationPanel();
  try { await loadNotifications(); } catch { /* keep cached list */ }
}

/* ---------- command palette ---------- */

function rememberRecent(entry) {
  const recent = storeGet('nexus-recent', []);
  const next = [entry, ...recent.filter((item) => item.id !== entry.id)].slice(0, 12);
  storeSet('nexus-recent', next);
}

function paletteActions() {
  return [
    { label: t('New session'), hint: t('Start an agent on an online host'), icon: 'plus', run: newSessionPicker },
    { label: t('Home'), hint: t('Overview and active work'), icon: 'home', run: () => { location.hash = '#/'; } },
    { label: t('Projects'), hint: t('Grouped by repository and worktree'), icon: 'projects', run: () => { location.hash = '#/projects'; } },
    { label: t('Sessions'), hint: t('All discovered native sessions'), icon: 'sessions', run: () => { location.hash = '#/sessions'; } },
    { label: t('Active runs'), hint: t('Terminal and structured runs'), icon: 'runs', run: () => { location.hash = '#/runs'; } },
    { label: t('Split workspace'), hint: t('Open two agent chats side by side'), icon: 'panelRight', run: () => { location.hash = '#/workspace'; } },
    { label: t('Hosts'), hint: t('Paired machines and agent inventory'), icon: 'hosts', run: () => { location.hash = '#/hosts'; } },
    { label: t('Search transcripts'), hint: t('Federated search across hosts'), icon: 'search', run: () => { location.hash = '#/search'; } },
    { label: t('Settings'), hint: t('Appearance, notifications, devices'), icon: 'settings', run: () => { location.hash = '#/settings'; } },
  ];
}

function paletteSession(s) {
  return {
    label: s.title || t('Untitled session'),
    hint: `${agentMeta(s.agent).label} · ${s.host_name} · ${projectName(s.cwd)}`,
    icon: 'sessions',
    run: () => { location.hash = `#/sessions/${s.id}`; },
  };
}

function renderPaletteResults(items) {
  const box = document.getElementById('palette-results');
  if (!box) return;
  state.palette.items = items;
  if (state.palette.index >= items.length) state.palette.index = Math.max(0, items.length - 1);
  box.innerHTML = items.length ? items.map((item, index) => `
    <button class="palette-row ${index === state.palette.index ? 'selected' : ''}" data-palette-index="${index}" type="button">
      <span class="palette-icon">${svgIcon(item.icon || 'sessions')}</span>
      <span class="palette-copy"><strong>${esc(item.label)}</strong><small>${esc(item.hint || '')}</small></span>
      ${index === state.palette.index ? '<kbd>↵</kbd>' : ''}
    </button>`).join('') : `<div class="palette-empty">${t('No matches.')}</div>`;
  box.querySelectorAll('[data-palette-index]').forEach((button) => button.addEventListener('click', () => {
    const item = items[Number(button.dataset.paletteIndex)];
    closePalette();
    item?.run();
  }));
}

function closePalette() {
  modalRoot.innerHTML = '';
  state.palette = null;
}

async function openPalette() {
  if (state.palette) return;
  state.palette = { index: 0, items: [], timer: null };
  const actions = paletteActions();
  const recent = storeGet('nexus-recent', []).map((entry) => ({
    ...entry,
    hint: entry.hint || t('Recently opened'),
    icon: 'sessions',
    run: () => { location.hash = entry.href || `#/sessions/${entry.id}`; },
  }));
  modalRoot.innerHTML = `
    <div class="palette-backdrop" data-palette-backdrop>
      <div class="palette" role="dialog" aria-modal="true" aria-label="Search or jump to">
        <div class="palette-input">${svgIcon('search')}<input id="palette-input" autocomplete="off" spellcheck="false" placeholder="${t('Search sessions, projects, hosts…')}" aria-label="${t('Search')}"><kbd>Esc</kbd></div>
        <div class="palette-results" id="palette-results"></div>
        <div class="palette-foot"><span>${t('↑↓ navigate')}</span><span>${t('↵ open')}</span><span>${t('Ctrl K toggle')}</span></div>
      </div>
    </div>`;
  renderPaletteResults([...actions, ...recent]);
  const input = document.getElementById('palette-input');
  const backdrop = modalRoot.querySelector('[data-palette-backdrop]');
  backdrop.addEventListener('click', (event) => { if (event.target === backdrop) closePalette(); });
  input.addEventListener('input', () => {
    clearTimeout(state.palette.timer);
    const query = input.value.trim();
    if (!query) {
      renderPaletteResults([...paletteActions(), ...recent]);
      return;
    }
    state.palette.timer = setTimeout(async () => {
      try {
        const { sessions } = await api(`/sessions?limit=12&q=${encodeURIComponent(query)}`);
        if (!state.palette) return;
        renderPaletteResults([...paletteActions().filter((a) => a.label.toLowerCase().includes(query.toLowerCase())), ...sessions.map(paletteSession)]);
      } catch { /* keep previous results */ }
    }, 160);
  });
  input.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') { closePalette(); return; }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const delta = event.key === 'ArrowDown' ? 1 : -1;
      const count = state.palette.items.length;
      if (count) state.palette.index = (state.palette.index + delta + count) % count;
      renderPaletteResults(state.palette.items);
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      const item = state.palette.items[state.palette.index];
      if (item) { closePalette(); item.run(); }
    }
  });
  input.focus();
}

/* ---------- session tabs ---------- */

function loadTabs() {
  state.openTabs = storeGet('nexus-tabs', []);
}

function saveTabs() {
  storeSet('nexus-tabs', state.openTabs);
  renderTabs();
}

function openTab(entry) {
  if (!entry?.id) return;
  const key = entry.key || entry.id;
  entry.key = key;
  state.openTabs = [entry, ...state.openTabs.filter((tab) => (tab.key || tab.id) !== key)].slice(0, 8);
  saveTabs();
}

function closeTab(key) {
  state.openTabs = state.openTabs.filter((tab) => (tab.key || tab.id) !== key);
  saveTabs();
}

function renderTabs() {
  const bar = document.getElementById('tabs-bar');
  if (!bar) return;
  if (state.embed || !state.openTabs.length) {
    bar.innerHTML = '';
    bar.hidden = true;
    return;
  }
  bar.hidden = false;
  bar.innerHTML = `<div class="tabs-scroll">${state.openTabs.map((tab) => {
    const key = tab.key || tab.id;
    const href = tab.href || (tab.kind === 'session' ? `#/sessions/${esc(tab.id)}` : `#/runs/${esc(tab.id)}`);
    const active = location.hash === href;
    return `<a class="tab ${active ? 'active' : ''}" href="${href}" title="${esc(tab.title || '')}">
      ${agentGlyph(tab.agent, 'tiny')}<span class="tab-title">${esc(tab.title || 'Untitled')}</span>
      <button class="tab-close" type="button" data-close-tab="${esc(key)}" aria-label="Close tab">${svgIcon('x', 13)}</button>
    </a>`;
  }).join('')}</div>`;
  bar.querySelectorAll('[data-close-tab]').forEach((button) => button.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    closeTab(button.dataset.closeTab);
  }));
}

function installGlobalUi() {
  if (state.uiInstalled) return;
  state.uiInstalled = true;
  loadTabs();
  document.addEventListener('keydown', (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      if (state.palette) closePalette(); else openPalette();
    }
    if (event.key === 'Escape') {
      if (state.palette) closePalette();
      if (modalRoot.firstElementChild) { closeModal(); return; }
      const panel = document.getElementById('notif-panel');
      if (panel) panel.hidden = true;
      document.getElementById('layout')?.classList.remove('mobile-sidebar-open');
      const chatPage = document.getElementById('chat-page');
      if (chatPage?.classList.contains('context-open')) {
        chatPage.classList.remove('context-open');
        storeSet('nexus-context-open', false);
      }
    }
  });
  document.addEventListener('click', (event) => {
    const panel = document.getElementById('notif-panel');
    const toggle = document.getElementById('notif-toggle');
    if (panel && !panel.hidden && !panel.contains(event.target) && !toggle?.contains(event.target)) panel.hidden = true;
  });
  document.addEventListener('click', (event) => {
    const button = event.target.closest('.md-copy');
    if (!button) return;
    const code = button.closest('.md-code')?.querySelector('code');
    if (!code) return;
    copyText(code.textContent || '');
    button.textContent = t('Copied');
    setTimeout(() => { button.textContent = t('Copy'); }, 1400);
  });
  document.addEventListener('click', (event) => {
    const button = event.target.closest('[data-copy]');
    if (!button) return;
    copyText(button.dataset.copy || '');
  });
  document.addEventListener('click', (event) => {
    const modeButton = event.target.closest('[data-diff-mode]');
    if (modeButton) {
      applyDiffMode(modeButton.closest('.diff-panel'), modeButton.dataset.diffMode);
      return;
    }
    const jump = event.target.closest('[data-diff-jump]');
    if (!jump) return;
    const panel = jump.closest('.diff-panel');
    if (!panel) return;
    const target = [...panel.querySelectorAll('[data-diff-file]')].find((node) => node.dataset.diffFile === jump.dataset.diffJump);
    target?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  });
  window.addEventListener('hashchange', renderTabs);
}

async function loadUserTheme() {
  try {
    state.settings = await api('/me/settings');
    window.NexusTheme?.apply(state.settings.theme || 'system');
  } catch { /* keep the local/system theme */ }
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
  if (h.startsWith('#/projects')) return 'projects';
  if (h.startsWith('#/sessions')) return 'sessions';
  if (h.startsWith('#/runs') || h.startsWith('#/workspace')) return 'runs';
  if (h.startsWith('#/search')) return 'search';
  if (h.startsWith('#/settings')) return 'settings';
  if (h.startsWith('#/devices') || h.startsWith('#/users') || h.startsWith('#/audit')) return 'settings';
  return 'overview';
}

/* ---------- auth ---------- */

function authFrame(inner, subtitle) {
  app.className = 'app-loading';
  app.innerHTML = `
    <div class="auth-wrap">
      <div class="auth-lang" role="group" aria-label="${t('Language')}">
        <button type="button" data-lang="en" class="${getLang() === 'en' ? 'active' : ''}">EN</button>
        <button type="button" data-lang="de" class="${getLang() === 'de' ? 'active' : ''}">DE</button>
      </div>
      <div class="auth-card">
        <div class="auth-brand">
          <div class="brand-mark">A</div>
          <div><h1>AgentNexus</h1><p>${esc(subtitle || t('Your agents. Your machines. One place.'))}</p></div>
        </div>
        <div class="card">${inner}</div>
        <p class="small muted" style="text-align:center;margin-top:14px">${t('Self-hosted · no telemetry · transcript content stays on your hosts')}</p>
      </div>
    </div>`;
  app.querySelectorAll('[data-lang]').forEach((button) => button.addEventListener('click', () => {
    setLang(button.dataset.lang);
    location.reload();
  }));
}

function renderAuth(mode, error = '') {
  state.me = null;
  disconnectWS();
  if (mode === 'setup') {
    authFrame(`
      <h2 class="mt-0">${t('Create the owner account')}</h2>
      <p class="hint mb-2">${t('This hub has no users yet. Choose a strong password; you can enroll a passkey or authenticator after signing in.')}</p>
      ${error ? `<div class="notice error mb-2">${esc(error)}</div>` : ''}
      <form class="form" id="setup-form">
        <label class="label">${t('Username')}<input class="input" name="username" aria-label="${t('Username')}" autocomplete="username" minlength="3" maxlength="64" required></label>
        <label class="label">${t('Password')}<input class="input" name="password" aria-label="${t('Password')}" type="password" autocomplete="new-password" minlength="10" required></label>
        <button class="btn primary block" type="submit" id="auth-submit">${t('Create hub')}</button>
      </form>`, t('First-run setup'));
    document.getElementById('setup-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const form = new FormData(event.target);
      const button = document.getElementById('auth-submit');
      button.disabled = true;
      button.innerHTML = `<span class="spinner"></span> ${t('Creating...')}`;
      try {
        const data = await api('/setup', { method: 'POST', body: {
          username: form.get('username'), password: form.get('password'),
        }, allow401: true });
        state.me = await api('/me');
        state.hubName = state.me.hub_name || state.hubName;
      await loadUserTheme();
        renderShell('overview');
        await route();
        if (data.totp_secret) {
          modal(`
            <h2>${t('Save your authenticator secret')}</h2>
            <p class="hint">${t('TOTP is optional until you confirm it. Scan this URI with an authenticator app, or copy the secret. You can enrol later in Devices.').replace('Devices', `<b>${t('Devices')}</b>`)}</p>
            <div class="secret">${esc(data.totp_secret)}</div>
            <p class="small muted mt-1">${t('otpauth URI')}</p>
            <div class="secret">${esc(data.totp_url)}</div>
            <div class="modal-actions">
              <button class="btn" type="button" id="copy-totp">${t('Copy secret')}</button>
              <button class="btn primary" type="button" data-close>${t('I saved it')}</button>
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
    <h2 class="mt-0">${t('Sign in')}</h2>
    <p class="hint mb-2">${t('Use your owner account. Authenticator code is required once TOTP is enabled.')}</p>
    ${error ? `<div class="notice error mb-2">${esc(error)}</div>` : ''}
    <form class="form" id="login-form">
      <label class="label">${t('Username')}<input class="input" name="username" aria-label="${t('Username')}" autocomplete="username" required autofocus></label>
      <label class="label">${t('Password')}<input class="input" name="password" aria-label="${t('Password')}" type="password" autocomplete="current-password" required></label>
      <label class="label">${t('Authenticator code')} <span class="muted">${t('(if enabled)')}</span><input class="input" name="totp" aria-label="${t('Authenticator code')}" inputmode="numeric" autocomplete="one-time-code" maxlength="6" pattern="[0-9]*"></label>
      <button class="btn primary block" type="submit" id="auth-submit">${t('Sign in')}</button>
      <button class="btn block" type="button" id="passkey-login">${t('Sign in with a passkey')}</button>
    </form>`, t('Sign in to your hub'));

  document.getElementById('passkey-login')?.addEventListener('click', async () => {
    const username = document.querySelector('[name=username]')?.value.trim();
    if (!username) { toast(t('Enter your username first'), true); return; }
    try {
      await passkeyLogin(username);
      state.me = await api('/me');
      state.hubName = state.me.hub_name || state.hubName;
      await loadUserTheme();
      renderShell('overview');
      await route();
    } catch (err) {
      renderAuth('login', err.message);
    }
  });

  document.getElementById('login-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = new FormData(event.target);
    const button = document.getElementById('auth-submit');
    button.disabled = true;
    button.innerHTML = `<span class="spinner"></span> ${t('Signing in...')}`;
    try {
      await api('/auth/login', { method: 'POST', body: {
        username: form.get('username'), password: form.get('password'), totp: form.get('totp') || '',
      }, allow401: true });
      state.me = await api('/me');
      state.hubName = state.me.hub_name || state.hubName;
      await loadUserTheme();
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
  state.openTabs = [];
  storeSet('nexus-tabs', []);
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
  const view = document.getElementById('view');
  if (view) view.className = 'view';
  const hash = location.hash || '#/';
  try {
    if (hash.startsWith('#/hosts')) return await pageHosts();
    if (hash.startsWith('#/projects')) return await pageProjects();
    if (hash.startsWith('#/sessions/') && hash.endsWith('/changes')) return await pageSessionChanges(hash.split('/')[2]);
    if (hash.startsWith('#/sessions/')) return await pageSession(hash.split('/')[2]);
    if (hash.startsWith('#/sessions')) return await pageSessions();
    if (hash === '#/runs') return await pageRuns();
    if (hash.startsWith('#/workspace')) return await pageWorkspace();
    if (hash.startsWith('#/runs/')) return await pageRun(hash.split('/')[2]);
    if (hash.startsWith('#/search')) return await pageSearch();
    if (hash.startsWith('#/settings')) return await pageSettings();
    if (hash.startsWith('#/devices')) return await pageDevices();
    if (hash.startsWith('#/users')) return await pageUsers();
    if (hash.startsWith('#/audit')) return await pageAudit();
    return await pageOverview();
  } catch (err) {
    if (err.status === 401) return;
    const target = document.getElementById('view');
    if (target) target.innerHTML = `<div class="notice error">${esc(err.message)}</div>`;
  } finally {
    renderTabs();
  }
}

async function pageOverview() {
  renderShell('overview');
  const view = setPage(t('Home'), t('Your coding agents across every paired machine.'), `<button class="btn primary small" id="overview-new">${t('New session')}</button><a class="btn small" href="#/runs">${t('Control room')}</a>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const [{ hosts }, { sessions }, { projects }, { runs }] = await Promise.all([
    api('/hosts'),
    api('/sessions?limit=80'),
    api('/projects?limit=200'),
    api('/runs'),
  ]);
  document.getElementById('overview-new').addEventListener('click', newSessionPicker);
  const online = hosts.filter((h) => h.status === 'online');
  const totalSessions = projects.reduce((sum, project) => sum + project.sessions, 0);
  const agents = new Set();
  hosts.forEach((h) => (h.agents || []).forEach((a) => agents.add(a.name)));
  const activeRuns = runs.slice(0, 6);
  const liveRuns = runs.filter((r) => r.state === 'running' || r.state === 'starting' || r.state === 'waiting_for_input');
  const openWorktrees = new Set(runs.map((r) => cwdKey(r.cwd)).filter(Boolean)).size;
  view.innerHTML = `
    <div class="home-head">
      <div>
        <h2 class="home-title">${t('Your workspace')}</h2>
        <p class="home-sub">${online.length ? `${countLabel(online.length, 'machine', 'machines')} ${t('online')}` : t('No machine online')} · ${countLabel(openWorktrees, 'worktree', 'worktrees')} ${t('with open runs')} · ${countLabel(agents.size, 'agent type', 'agent types')}</p>
      </div>
      <div class="home-stats">
        <span><strong>${totalSessions}</strong> ${t(totalSessions === 1 ? 'session' : 'sessions')}</span>
        <span><strong>${projects.length}</strong> ${t(projects.length === 1 ? 'project' : 'projects')}</span>
        <span><strong>${liveRuns.length}</strong> ${t('working')}</span>
      </div>
    </div>

    <section class="home-section">
      <div class="section-head"><h2>${t('Agent activity')}</h2><a class="small" href="#/runs">${t('Open control room')}</a></div>
      ${activeRuns.length ? `<div class="run-board">${activeRuns.map(runCard).join('')}</div>` : `<div class="home-empty"><span>${svgIcon('bot', 22)}</span><div><strong>${t('No open agent runs')}</strong><p>${t('Resume a native session or start a new agent on an online host.')}</p></div><button class="btn primary small" id="home-new">${t('New session')}</button></div>`}
    </section>

    <div class="home-columns">
      <section>
        <div class="section-head"><h2>${t('Projects')}</h2><a class="small" href="#/projects">${t('All projects')}</a></div>
        ${projects.length ? `<div class="project-list">${projects.slice(0, 7).map(projectRow).join('')}</div>` : `<div class="card empty"><strong>${t('No projects yet')}</strong>${t('Pair a host and native sessions will be discovered automatically.')}</div>`}
      </section>
      <section>
        <div class="section-head"><h2>${t('Recent sessions')}</h2><a class="small" href="#/sessions">${t('All sessions')}</a></div>
        ${sessions.length ? `<div class="session-list">${sessions.slice(0, 7).map(sessionRow).join('')}</div>` : `<div class="card empty"><strong>${t('No sessions')}</strong>${t('Pair a host that already has coding-agent history.')}</div>`}
      </section>
    </div>

    <section class="home-section">
      <div class="section-head"><h2>${t('Hosts')}</h2><a class="small" href="#/hosts">${t('Manage hosts')}</a></div>
      ${hosts.length ? `<div class="host-strip">${hosts.map((h) => `<a class="host-pill" href="#/hosts"><span class="side-state ${h.status === 'online' ? 'online' : ''}"></span><span>${esc(h.name)}</span><small>${esc(h.os)}/${esc(h.arch)} · ${(h.agents || []).length} ${t((h.agents || []).length === 1 ? 'agent' : 'agents')}</small></a>`).join('')}</div>` : `<div class="card empty"><strong>${t('No hosts')}</strong>${t('Pair your first machine to start discovering sessions.')}</div>`}
    </section>`;
  document.getElementById('home-new')?.addEventListener('click', newSessionPicker);
}

function runCard(run) {
  return `<a class="run-card" href="#/runs/${esc(run.id)}">
    <div class="run-card-head">${agentGlyph(run.agent)}${stateBadge(run.state)}<span class="run-elapsed">${fmtDuration(run.started_at)}</span></div>
    <div class="run-card-title">${esc(run.session_title || t('Untitled session'))}</div>
    <div class="run-card-meta">${esc(projectName(run.cwd))} <span>·</span> ${esc(run.host_name || run.host_id)} <span>·</span> ${esc(run.mode)}</div>
  </a>`;
}

function projectRow(p) {
  return `<a class="project-row" href="#/sessions?project=${encodeURIComponent(p.cwd)}&host=${encodeURIComponent(p.host_id)}">
    <span class="project-row-icon">${svgIcon('folder', 17)}</span>
    <span class="project-row-main"><strong>${esc(p.name)}</strong><small>${esc(p.host_name)}${p.branch ? ` · ${esc(p.branch)}` : ''} · ${countLabel(p.sessions, 'session', 'sessions')}</small></span>
    <span class="project-row-agents">${(p.agents || []).slice(0, 3).map((agent) => agentGlyph(agent, 'tiny')).join('')}${p.active ? `<span class="active-pill">${t('{n} active', { n: p.active })}</span>` : ''}</span>
    <span class="project-row-time">${esc(fmtRelative(p.last_at))}</span>
  </a>`;
}

function hostRowCompact(h) {
  return `<a class="list-row" href="#/hosts">
    <div><div class="title">${esc(h.name)}</div><div class="sub"><span>${esc(h.os)}/${esc(h.arch)}</span><span>${countLabel((h.agents || []).length, 'agent', 'agents')}</span></div></div>
    <div class="right">${badge(h.status === 'online' ? 'online' : 'offline', h.status)}</div>
  </a>`;
}

function sessionRow(s) {
  return `<a class="session-row" href="#/sessions/${esc(s.id)}">
    ${agentGlyph(s.agent)}
    <span class="session-row-main">
      <strong>${esc(s.title || t('Untitled session'))}</strong>
      <small>${esc(projectName(s.cwd))} <span>·</span> ${esc(s.host_name)} <span>·</span> ${esc(fmtRelative(s.updated_at))}</small>
    </span>
    ${s.git_branch ? `<span class="branch-chip">${svgIcon('branch', 12)}${esc(s.git_branch)}</span>` : ''}
    ${stateBadge(s.state)}
  </a>`;
}

async function pageHosts() {
  renderShell('hosts');
  const view = setPage(t('Hosts'), t('Machines running nexusd. Pairing is always initiated from this hub.'), `<button class="btn primary small" id="pair-host">${t('Pair a host')}</button>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const [{ hosts }] = await Promise.all([api('/hosts')]);
  document.getElementById('pair-host').addEventListener('click', pairHostDialog);
  state.wsHandlers.add((msg) => {
    if (msg.type === 'host.updated') pageHosts().catch(() => {});
    if (msg.type === 'sessions.changed') { /* no-op on hosts page */ }
  });
  if (!hosts.length) {
    view.innerHTML = `<div class="card empty"><strong>${t('No hosts paired')}</strong>${t('Create a one-time code, then run')} <code>nexusd pair --hub ${esc(location.origin)} --code &lt;code&gt;</code> ${t('on the machine.')}</div>`;
    return;
  }
  view.innerHTML = `<div class="list">${hosts.map((h) => `
    <div class="host-row">
      <div class="host-id">
        <div class="host-name"><span class="side-dot ${h.status === 'online' ? 'on' : ''}"></span>${esc(h.name)}${badge(t(h.pending ? 'pending' : (h.status === 'online' ? 'online' : 'offline')), h.pending ? 'waiting_for_input' : h.status)}${h.update?.available ? `<span class="badge waiting_for_input" title="${t('nexusd update available')}">${t('update')} ${esc(h.update.latest || '')}</span>` : ''}</div>
        <div class="sub">${esc(h.os)}/${esc(h.arch)} · nexusd ${esc(h.version || 'unknown')} · ${t('seen')} ${esc(fmtRelative(h.last_seen))}</div>
      </div>
      <div class="agent-chips">${(h.agents || []).map((a) => `<span class="chip"><b>${esc(a.name)}</b> ${esc(agentVersion(a.name, a.version))}</span>`).join('') || `<span class="chip">${t('no agents')}</span>`}</div>
      <div class="host-actions">
        <button class="btn small ghost" data-host-details="${esc(h.id)}">${t('Details')}</button>
        <button class="btn small" data-new-session="${esc(h.id)}" ${h.status !== 'online' ? 'disabled' : ''}>${t('New session')}</button>
        <button class="btn small" data-share-host="${esc(h.id)}">${t('Share')}</button>
        <button class="btn small danger" data-revoke-host="${esc(h.id)}">${t('Revoke')}</button>
      </div>
      ${h.pending ? `<div class="host-pending"><span>${t('Compare SAS with the daemon.')}</span><span class="secret">${esc(h.sas)}</span><button class="btn primary small" data-confirm-host="${esc(h.id)}">${t('Confirm host')}</button></div>` : ''}
    </div>`).join('')}</div>`;
  view.querySelectorAll('[data-new-session]').forEach((b) => b.addEventListener('click', () => newSessionDialog(hosts.find((h) => h.id === b.dataset.newSession))));
  view.querySelectorAll('[data-confirm-host]').forEach((b) => b.addEventListener('click', async () => {
    try {
      await api(`/hosts/${b.dataset.confirmHost}/confirm`, { method: 'POST' });
      toast(t('Host confirmed'));
      pageHosts();
    } catch (err) {
      toast(err.message, true);
    }
  }));
  view.querySelectorAll('[data-host-details]').forEach((b) => b.addEventListener('click', () => {
    const h = hosts.find((host) => host.id === b.dataset.hostDetails);
    if (!h) return;
    const policy = h.policy || {};
    modal(`
      <div class="modal-title-row"><h2>${esc(h.name)}</h2>${badge(t(h.pending ? 'pending' : h.status), h.pending ? 'waiting_for_input' : h.status)}</div>
      <p class="hint">${t('Host identity and the policy enforced locally by nexusd. The hub cannot override these limits.')}</p>
      <div class="meta-list">
        <div><span>${t('Host ID')}</span><strong class="mono-small">${esc(h.id)}</strong></div>
        <div><span>${t('Platform')}</span><strong>${esc(h.os)}/${esc(h.arch)}</strong></div>
        <div><span>${t('Daemon')}</span><strong>${esc(h.version || 'unknown')}</strong></div>
        <div><span>${t('Last seen')}</span><strong>${esc(fmtTime(h.last_seen))}</strong></div>
        <div><span>${t('Allowed roots')}</span><strong>${esc((policy.allowed_roots || []).join(', ') || t('not reported'))}</strong></div>
        <div><span>${t('Allowed agents')}</span><strong>${esc((policy.allowed_agents || []).join(', ') || t('all detected'))}</strong></div>
        <div><span>${t('New / fork')}</span><strong>${policy.allow_new_sessions === false ? t('new blocked') : t('new allowed')} · ${policy.allow_fork === false ? t('fork blocked') : t('fork allowed')}</strong></div>
      </div>
      <div class="aside-label mt-2">${t('Detected agents')}</div>
      <div class="agent-chips">${(h.agents || []).map((a) => `<span class="chip"><b>${esc(a.name)}</b> ${esc(agentVersion(a.name, a.version))}</span>`).join('') || `<span class="chip">${t('none')}</span>`}</div>
      <div class="aside-label mt-2">${t('Updates')}</div>
      <div id="host-update-result" class="host-update-result">${h.update ? `
        <div class="meta-list">
          <div><span>${t('Installed')}</span><strong>${esc(h.update.current || h.version || t('unknown'))}</strong></div>
          <div><span>${t('Latest')}</span><strong>${esc(h.update.latest || t('unknown'))}</strong></div>
          <div><span>${t('Status')}</span><strong>${h.update.error ? t('check failed') : (h.update.available ? t('update available') : t('up to date'))}</strong></div>
        </div>
        ${h.update.error ? `<div class="notice error small mt-1">${esc(h.update.error)}</div>` : ''}
        ${h.update.available ? `<div class="notice small mt-1">${t('Run on the host:')} <code>nexusd update --install</code> ${t('(refuses while runs are active)')}</div>` : ''}
      ` : `<p class="aside-note">${t('Check the GitHub release feed. Installation stays on the host:')} <code>nexusd update --install</code>.</p>`}</div>
      <div class="modal-actions"><button class="btn small" id="host-update-check" type="button">${t('Check for updates')}</button><button class="btn" type="button" data-close>${t('Close')}</button></div>`);
    document.getElementById('host-update-check')?.addEventListener('click', async () => {
      const box = document.getElementById('host-update-result');
      const button = document.getElementById('host-update-check');
      if (!box || !button) return;
      button.disabled = true;
      box.innerHTML = `<div class="empty compact"><span class="spinner"></span>${t('Checking...')}</div>`;
      try {
        const info = await api(`/hosts/${encodeURIComponent(h.id)}/update`);
        box.innerHTML = `
          <div class="meta-list">
            <div><span>${t('Installed')}</span><strong>${esc(info.current || h.version || t('unknown'))}</strong></div>
            <div><span>${t('Latest')}</span><strong>${esc(info.latest || t('unknown'))}</strong></div>
            <div><span>${t('Status')}</span><strong>${info.available ? t('update available') : t('up to date')}</strong></div>
          </div>
          ${info.available ? `<div class="notice small mt-1">Run on the host: <code>nexusd update --install</code> (refuses while runs are active).</div>` : ''}`;
      } catch (err) {
        box.innerHTML = `<div class="notice error small">${esc(err.message)}</div><p class="aside-note mt-1">${t('Private releases require')} <code>NEXUS_GITHUB_TOKEN</code> ${t('with Contents: read on the host.')}</p>`;
      } finally {
        button.disabled = false;
      }
    });
  }));
  view.querySelectorAll('[data-share-host]').forEach((b) => b.addEventListener('click', () => shareHostDialog(hosts.find((h) => h.id === b.dataset.shareHost))));
  view.querySelectorAll('[data-revoke-host]').forEach((b) => b.addEventListener('click', () => {
    confirmDanger('Revoking this host closes its connection and fails active runs. The daemon must be paired again.', async () => {
      await api(`/hosts/${b.dataset.revokeHost}`, { method: 'DELETE' });
      toast(t('Host revoked'));
      pageHosts();
    });
  }));
}

async function pairHostDialog() {
  try {
    await ensureStepUp();
  } catch {
    return;
  }
  modal(`
    <h2>${t('Pair a host')}</h2>
    <p class="hint">${t('The code is single-use and expires after 10 minutes. The host connects outbound; no inbound ports are required.')}</p>
    <div class="empty" id="pair-code"><span class="spinner"></span></div>
    <div id="pair-steps"></div>
    <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Close')}</button></div>`);
  api('/hosts/pairing-codes', { method: 'POST' }).then((data) => {
    const box = document.getElementById('pair-code');
    if (!box) return;
    box.className = 'secret';
    box.textContent = data.code;
    const origin = location.origin.replace(/^http/, 'ws');
    document.getElementById('pair-steps').innerHTML = `
      <p class="small muted mt-2">${t('Run this on the machine you want to add:')}</p>
      <div class="secret">nexusd pair --hub ${esc(location.origin)} --code ${esc(data.code)}</div>
      <div class="split mt-1">
        <button class="btn small" id="copy-code">${t('Copy code')}</button>
        <button class="btn small" id="copy-command">${t('Copy command')}</button>
      </div>
      <p class="small muted mt-2">${t('Then install the per-user service:')} <code>nexusd service install</code>. ${t('The host is accepted by this hub at')} <code>${esc(origin)}/v1/host/connect</code>.</p>`;
    document.getElementById('copy-code').addEventListener('click', () => copyText(data.code));
    document.getElementById('copy-command').addEventListener('click', () => copyText(`nexusd pair --hub ${location.origin} --code ${data.code}`));
  }).catch((err) => {
    const box = document.getElementById('pair-code');
    if (box) { box.className = 'notice error'; box.textContent = err.message; }
  });
}

async function newSessionPicker() {
  try {
    const { hosts } = await api('/hosts');
    const online = hosts.filter((h) => h.status === 'online');
    if (!online.length) {
      toast(t('No online host. Pair a host first.'), true);
      return;
    }
    if (online.length === 1) {
      await newSessionDialog(online[0]);
      return;
    }
    modal(`
      <h2>${t('New session')}</h2>
      <p class="hint">${t('Choose the machine that should run the agent.')}</p>
      <form class="form" id="host-picker-form">
        <label class="label">${t('Host')}<select class="select" name="host">${online.map((h) => `<option value="${esc(h.id)}">${esc(h.name)} · ${esc(h.os)}/${esc(h.arch)}</option>`).join('')}</select></label>
        <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Cancel')}</button><button class="btn primary" type="submit">${t('Continue')}</button></div>
      </form>`);
    document.getElementById('host-picker-form').addEventListener('submit', (event) => {
      event.preventDefault();
      const id = new FormData(event.target).get('host');
      const host = online.find((h) => h.id === id);
      closeModal();
      if (host) newSessionDialog(host);
    });
  } catch (err) { toast(err.message, true); }
}

async function newSessionDialog(host) {
  const agents = (host?.agents || []).filter((a) => a.supported);
  let projects = [];
  try {
    const { sessions } = await api(`/sessions?host=${encodeURIComponent(host.id)}&limit=200`);
    projects = [...new Set(sessions.map((s) => s.cwd).filter(Boolean))].slice(0, 40);
  } catch { /* the free-text field still works */ }
  modal(`
    <h2>${t('New session on')} ${esc(host?.name || 'host')}</h2>
    <p class="hint">${t('The agent starts in a directory permitted by the host policy. Permission-bypass flags are rejected on the host.')}</p>
    <form class="form" id="new-session-form">
      <label class="label">${t('Agent')}<select class="select" name="agent" id="new-agent">${agents.map((a) => `<option value="${esc(a.name)}" data-structured="${(a.capabilities || []).includes('structured') ? '1' : '0'}">${esc(agentMeta(a.name).label)} — ${esc(agentVersion(a.name, a.version))}</option>`).join('')}</select></label>
      <label class="label">${t('Mode')}<select class="select" name="mode" id="new-mode"><option value="structured">${t('Structured chat')}</option><option value="terminal">${t('Terminal')}</option></select></label>
      <label class="label">${t('Working directory')}<input class="input" name="cwd" list="project-options" placeholder="${t('C:\\Users\\me\\project or /home/me/project')}" required><datalist id="project-options">${projects.map((p) => `<option value="${esc(p)}"></option>`).join('')}</datalist></label>
      <div class="notice info">${t('The host enforces its own')} <code>allowed_roots</code>; ${t('a path outside them is rejected even if this form accepts it.')}</div>
      <div class="modal-actions">
        <button class="btn ghost" type="button" data-close>${t('Cancel')}</button>
        <button class="btn primary" type="submit">${t('Start session')}</button>
      </div>
    </form>`);
  const agentSelect = document.getElementById('new-agent');
  const modeSelect = document.getElementById('new-mode');
  const syncMode = () => {
    const option = agentSelect.selectedOptions[0];
    const structured = option?.dataset.structured === '1';
    modeSelect.querySelector('option[value=structured]').disabled = !structured;
    if (!structured) modeSelect.value = 'terminal';
  };
  agentSelect.addEventListener('change', syncMode);
  modeSelect.value = agentSelect.selectedOptions[0]?.dataset.structured === '1' ? 'structured' : 'terminal';
  syncMode();
  document.getElementById('new-session-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = new FormData(event.target);
    try {
      const run = await api(`/hosts/${host.id}/runs`, { method: 'POST', body: {
        agent: form.get('agent'), cwd: form.get('cwd'), mode: form.get('mode'), cols: 120, rows: 32,
      } });
      closeModal();
      location.hash = `#/runs/${run.id}`;
    } catch (err) {
      toast(err.message, true);
    }
  });
}

async function pageProjects() {
  renderShell('projects');
  const view = setPage(t('Projects'), t('Working directories discovered from native sessions, grouped by host and worktree.'), `<button class="btn primary small" id="projects-new">${t('New session')}</button>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const { projects } = await api('/projects?limit=500');
  document.getElementById('projects-new').addEventListener('click', newSessionPicker);
  const render = (filter = '') => {
    const list = document.getElementById('project-list');
    if (!list) return;
    const query = filter.trim().toLowerCase();
    const filtered = projects.filter((p) => !query || `${p.name} ${p.cwd} ${p.host_name} ${p.branch}`.toLowerCase().includes(query));
    list.innerHTML = filtered.length ? `<div class="project-grid">${filtered.map((p) => `
      <a class="project-card" href="#/sessions?project=${encodeURIComponent(p.cwd)}&host=${encodeURIComponent(p.host_id)}">
        <div class="project-icon">${svgIcon('folder', 20)}</div>
        <div class="project-main">
          <div class="project-title">${esc(p.name)}${p.active ? `<span class="active-pill">${t('{n} active', { n: p.active })}</span>` : ''}${p.active > 1 ? `<span class="warn-pill" title="${t('More than one agent is active in this working tree')}">${t('shared worktree')}</span>` : ''}</div>
          <div class="project-path">${esc(p.cwd)}</div>
          <div class="project-meta">${esc(p.host_name)}${p.branch ? ` · ${svgIcon('branch', 12)} ${esc(p.branch)}` : ''} · ${t('{n} sessions', { n: p.sessions })} · ${esc(fmtRelative(p.last_at))}</div>
        </div>
        <div class="project-agents">${(p.agents || []).slice(0, 4).map((agent) => agentGlyph(agent, 'small')).join('')}</div>
        <span class="project-arrow">${svgIcon('chevronRight')}</span>
      </a>`).join('')}</div>` : `<div class="card empty"><strong>${t('No projects match')}</strong>${t('Try a different filter or wait for a host to reconnect.')}</div>`;
  };
  view.innerHTML = `
    <div class="toolbar">
      <input class="input grow" id="projects-filter" placeholder="Filter projects, paths, hosts or branches…">
      <span class="toolbar-count">${t('{n} worktrees', { n: projects.length })}</span>
    </div>
    <div id="project-list"></div>`;
  document.getElementById('projects-filter').addEventListener('input', (event) => render(event.target.value));
  render();
}

async function pageSessions() {
  renderShell('sessions');
  const params = new URLSearchParams(location.hash.split('?')[1] || '');
  const [{ hosts }] = await Promise.all([api('/hosts')]);
  const view = setPage(t('Sessions'), t('Discovered natively from each host; nothing is copied or rewritten.'), `<button class="btn primary small" id="sessions-new">${t('New session')}</button>`);
  view.innerHTML = `
    <div class="filterbar">
      <label class="search-field">${svgIcon('search')}<input class="input" id="filter-q" placeholder="${t('Filter title, project or path…')}" value="${esc(params.get('q') || '')}"></label>
      <select class="select" id="filter-agent" aria-label="${t('Filter by agent')}"><option value="">${t('All agents')}</option>${['claude', 'codex', 'opencode', 'gemini'].map((a) => `<option value="${a}" ${params.get('agent') === a ? 'selected' : ''}>${agentMeta(a).label}</option>`).join('')}</select>
      <select class="select" id="filter-host" aria-label="${t('Filter by host')}"><option value="">${t('All hosts')}</option>${hosts.map((h) => `<option value="${esc(h.id)}" ${params.get('host') === h.id ? 'selected' : ''}>${esc(h.name)}</option>`).join('')}</select>
      <select class="select" id="filter-state" aria-label="${t('Filter by state')}"><option value="">${t('Any state')}</option>${['running', 'waiting_for_input', 'idle', 'completed', 'failed', 'unknown'].map((s) => `<option value="${s}" ${params.get('state') === s ? 'selected' : ''}>${t(s.replaceAll('_', ' '))}</option>`).join('')}</select>
      <span class="filter-count" id="session-count"></span>
    </div>
    <div class="project-filter" id="project-filter" hidden></div>
    <div id="session-list"><div class="empty"><span class="spinner"></span></div></div>
    <div class="load-more" id="session-more"></div>`;
  document.getElementById('sessions-new').addEventListener('click', newSessionPicker);
  let offset = 0;
  let sessions = [];
  let hasMore = false;
  const project = params.get('project') || '';
  const clearProject = () => {
    const node = document.getElementById('project-filter');
    if (!node) return;
    node.hidden = !project;
    node.innerHTML = project ? `<span>${svgIcon('folder', 13)}${esc(projectName(project))}</span><code>${esc(project)}</code><button class="icon-btn" id="clear-project" type="button" aria-label="${t('Clear project filter')}">${svgIcon('x', 13)}</button>` : '';
    document.getElementById('clear-project')?.addEventListener('click', () => {
      const url = new URL(location.href);
      url.hash = '#/sessions';
      location.href = url.toString();
    });
  };
  const render = () => {
    const list = document.getElementById('session-list');
    if (!list) return;
    document.getElementById('session-count').textContent = t('{n} shown', { n: `${sessions.length}${hasMore ? '+' : ''}` });
    if (!sessions.length) {
      list.innerHTML = `<div class="card empty"><strong>${t('No sessions match')}</strong>${t('Try another filter, or wait for a host to reconnect.')}</div>`;
      document.getElementById('session-more').innerHTML = '';
      return;
    }
    const groups = new Map();
    for (const s of sessions) {
      const key = s.repo_url ? `repo:${s.repo_url.toLowerCase()}` : `dir:${s.host_id}|${cwdKey(s.cwd)}`;
      if (!groups.has(key)) groups.set(key, { name: projectName(s.cwd), cwd: s.cwd, hosts: new Set(), branches: new Set(), roots: new Set(), rows: [] });
      const group = groups.get(key);
      group.hosts.add(s.host_name || s.host_id);
      if (s.git_branch) group.branches.add(s.git_branch);
      if (s.repo_root) group.roots.add(cwdKey(s.repo_root));
      group.rows.push(s);
    }
    list.innerHTML = [...groups.values()].map((g) => `
      <section class="session-group">
        <div class="session-group-head">
          <span>${svgIcon('folder', 14)}<strong>${esc(g.name)}</strong><code>${esc(g.cwd)}</code></span>
          <span>${[...g.branches].slice(0, 3).map((b) => `<span class="branch-chip">${svgIcon('branch', 12)}${esc(b)}</span>`).join('')}${g.roots.size > 1 ? `<span class="muted">${t('{n} worktrees', { n: g.roots.size })}</span>` : ''}<span class="muted">${esc([...g.hosts].join(', '))} &middot; ${g.rows.length}</span></span>
        </div>
        <div class="session-list">${g.rows.map((s) => `
          <a class="session-row" href="#/sessions/${esc(s.id)}">
            ${agentGlyph(s.agent)}
            <span class="session-row-main"><strong>${esc(s.title || t('Untitled session'))}</strong><small>${g.hosts.size > 1 ? `${esc(s.host_name || s.host_id)} &middot; ` : ''}${esc(fmtRelative(s.updated_at))} &middot; ${countLabel(s.message_count || 0, 'message', 'messages')}${g.roots.size > 1 && s.repo_root ? ` &middot; ${esc(projectName(s.repo_root))}` : ''}</small></span>
            ${stateBadge(s.state)}
          </a>`).join('')}</div>
      </section>`).join('');
    document.getElementById('session-more').innerHTML = hasMore ? `<button class="btn" id="load-more-sessions">${t('Load more')}</button>` : '';
    document.getElementById('load-more-sessions')?.addEventListener('click', () => load(false).catch((e) => toast(e.message, true)));
  };
  const load = async (reset = true) => {
    if (reset) { offset = 0; sessions = []; document.getElementById('session-list').innerHTML = '<div class="empty"><span class="spinner"></span></div>'; }
    const q = new URLSearchParams();
    const query = document.getElementById('filter-q').value.trim();
    if (query) q.set('q', query);
    const agent = document.getElementById('filter-agent').value;
    if (agent) q.set('agent', agent);
    const host = document.getElementById('filter-host').value;
    if (host) q.set('host', host);
    const stateFilter = document.getElementById('filter-state').value;
    if (stateFilter) q.set('state', stateFilter);
    if (project) q.set('project', project);
    q.set('limit', '100');
    q.set('offset', String(offset));
    const data = await api('/sessions?' + q.toString());
    sessions = sessions.concat(data.sessions || []);
    offset += (data.sessions || []).length;
    hasMore = Boolean(data.has_more);
    const historyParams = new URLSearchParams(q);
    historyParams.delete('limit');
    historyParams.delete('offset');
    history.replaceState(null, '', '#/sessions' + (historyParams.toString() ? '?' + historyParams.toString() : ''));
    render();
  };
  let filterTimer = null;
  const schedule = () => { clearTimeout(filterTimer); filterTimer = setTimeout(() => load().catch((e) => toast(e.message, true)), 220); };
  document.getElementById('filter-q').addEventListener('input', schedule);
  for (const id of ['filter-agent', 'filter-host', 'filter-state']) document.getElementById(id).addEventListener('change', () => load().catch((e) => toast(e.message, true)));
  state.wsHandlers.add((msg) => { if (msg.type === 'sessions.changed') schedule(); });
  clearProject();
  await load();
}

async function pageSession(id) {
  renderShell('sessions');
  const view = setPage(t('Session'), t('Loading transcript…'), `<a class="btn small" href="#/sessions">${t('Back')}</a>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const s = await api(`/sessions/${id}`);
  const agent = agentMeta(s.agent);
  openTab({ id: s.id, key: s.id, kind: 'session', href: `#/sessions/${s.id}`, title: s.title || 'Untitled session', agent: s.agent });
  rememberRecent({ id: s.id, title: s.title || 'Untitled session', agent: s.agent, hint: `${agent.label} · ${s.host_name} · ${projectName(s.cwd)}`, href: `#/sessions/${s.id}` });
  const title = s.title || 'Session';
  setPage(title, `${projectName(s.cwd)} · ${agent.label} · ${s.host_name}`, `
    <button class="btn small" id="favorite-session">${svgIcon('star')} ${state.favorites.has(s.id) ? t('Pinned') : t('Pin')}</button>
    <button class="btn small" id="resume-session">${svgIcon('terminal')} ${t('Terminal')}</button>
    ${s.agent === 'opencode' || s.agent === 'codex' || s.agent === 'claude' ? `<button class="btn small primary" id="structured-session">${svgIcon('bot')} ${t('Open chat')}</button>` : ''}
    <a class="btn small ghost" href="#/sessions">${t('Back')}</a>`);
  const favorite = async (on) => {
    await api(`/sessions/${s.id}/favorite`, { method: on ? 'PUT' : 'DELETE' });
    if (on) state.favorites.add(s.id); else state.favorites.delete(s.id);
    const button = document.getElementById('favorite-session');
    if (button) button.innerHTML = `${svgIcon('star')} ${on ? t('Pinned') : t('Pin')}`;
    loadSidebar().catch(() => {});
  };
  document.getElementById('favorite-session').addEventListener('click', () => favorite(!state.favorites.has(s.id)).catch((e) => toast(e.message, true)));
  document.getElementById('resume-session').addEventListener('click', () => startRun(s, false, 'terminal'));
  document.getElementById('structured-session')?.addEventListener('click', () => startRun(s, false, 'structured'));
  view.innerHTML = `
    <div class="detail-layout">
      <section class="detail-main">
        <div class="session-banner">
          ${agentGlyph(s.agent)}
          <div class="session-banner-main">
            <strong>${esc(title)}</strong>
            <span class="session-banner-path">${esc(s.cwd || 'unknown directory')}</span>
          </div>
          ${stateBadge(s.state)}
          <span class="session-banner-meta">${s.message_count || 0} ${t((s.message_count || 0) === 1 ? 'message' : 'messages')} · ${t('updated')} ${esc(fmtRelative(s.updated_at))}</span>
        </div>
        ${s.warning ? `<div class="notice mt-1">${esc(s.warning)}</div>` : ''}
        <div id="transcript" class="transcript"><div class="empty"><span class="spinner"></span></div></div>
        <div class="load-more" id="load-more-wrap"></div>
      </section>
      <aside class="detail-aside">
        <div class="aside-section">
          <div class="aside-label">${t('Session')}</div>
          <div class="meta-list">
            <div><span>${t('Agent')}</span><strong>${esc(agent.label)}</strong></div>
            <div><span>${t('Host')}</span><strong>${esc(s.host_name)}</strong></div>
            <div><span>${t('Created')}</span><strong>${esc(fmtTime(s.created_at))}</strong></div>
            <div><span>${t('Updated')}</span><strong>${esc(fmtTime(s.updated_at))}</strong></div>
          </div>
        </div>
        <div class="aside-section">
          <div class="aside-label">${t('Native identity')}</div>
          <div class="copy-line"><code>${esc(s.native_id || s.id)}</code><button class="icon-btn" data-copy="${esc(s.native_id || s.id)}" title="${t('Copy native id')}">${svgIcon('copy', 14)}</button></div>
          <div class="copy-line"><code>${esc(s.cwd || '')}</code><button class="icon-btn" data-copy="${esc(s.cwd || '')}" title="${t('Copy working directory')}">${svgIcon('copy', 14)}</button></div>
          ${s.git_branch ? `<div class="branch-chip wide">${svgIcon('branch', 13)}${esc(s.git_branch)}</div>` : ''}
          ${s.repo_url ? `<div class="aside-repo">${repoLink(s.repo_url)}${s.repo_root && cwdKey(s.repo_root) !== cwdKey(s.cwd) ? `<span class="muted">${t('worktree')}</span>` : ''}</div>` : ''}
        </div>
        <div class="aside-section">
          <div class="aside-label">${t('Actions')}</div>
          <div class="aside-actions">
            <a class="btn small" id="git-session" href="#/sessions/${s.id}/changes">${svgIcon('git')} ${t('Review changes')}</a>
            ${s.agent === 'codex' || s.agent === 'claude' ? `<button class="btn small" id="fork-session">${t('Fork session')}</button>` : ''}
            <a class="btn small" href="/api/v1/sessions/${encodeURIComponent(s.id)}/export?format=markdown">${svgIcon('file')} ${t('Export Markdown')}</a>
            <a class="btn small" href="/api/v1/sessions/${encodeURIComponent(s.id)}/export?format=json">${t('Export JSON')}</a>
          </div>
        </div>
        <div class="aside-section">
          <div class="aside-label">${t('Native continuity')}</div>
          <p class="aside-note">${t("Continuing this conversation reopens the agent's own native session. The original CLI can still resume it afterwards.")}</p>
        </div>
      </aside>
    </div>`;
  document.getElementById('fork-session')?.addEventListener('click', () => startRun(s, true, 'terminal'));
  let offset = 0;
  const append = async () => {
    const box = document.getElementById('transcript');
    const tr = await api(`/sessions/${encodeURIComponent(id)}/transcript?offset=${offset}&limit=100`);
    if (offset === 0) box.innerHTML = '';
    box.insertAdjacentHTML('beforeend', tr.messages.length ? renderTranscriptMessages(tr.messages, s.agent) : (!offset ? `<div class="empty"><strong>${t('No readable messages')}</strong>${t('The host could not parse this transcript.')}</div>` : ''));
    offset = tr.next || 0;
    document.getElementById('load-more-wrap').innerHTML = tr.next ? `<button class="btn" id="load-more">${t('Load earlier messages')}</button>` : '';
    document.getElementById('load-more')?.addEventListener('click', () => append().catch((e) => toast(e.message, true)));
  };
  await append();
}

async function pageSessionChanges(id) {
  renderShell('sessions');
  const s = await api(`/sessions/${id}`);
  const agent = agentMeta(s.agent);
  const title = s.title || 'Session';
  openTab({ id: `${s.id}:changes`, key: `${s.id}:changes`, kind: 'session', href: `#/sessions/${s.id}/changes`, title: `${title} / Changes`, agent: s.agent });
  const view = setPage(t('Changes'), `${title} · ${projectName(s.cwd)}`, `
    <a class="btn small" href="#/sessions/${s.id}">${svgIcon('sessions')} ${t('Back to session')}</a>
  `);
  let git;
  try {
    git = await api(`/sessions/${s.id}/git`);
  } catch (err) {
    view.innerHTML = `<div class="notice error">${esc(err.message)}</div>`;
    return;
  }
  view.innerHTML = `
    <div class="changes-page changes-main">
      <div class="session-banner">
        ${agentGlyph(s.agent)}
        <div class="session-banner-main">
          <strong>${esc(title)}</strong>
          <span class="session-banner-path">${esc(s.cwd || 'unknown directory')} · ${esc(agent.label)} · ${esc(s.host_name)}</span>
        </div>
        ${stateBadge(s.state)}
        ${git.branch ? `<span class="branch-chip">${svgIcon('branch', 13)}${esc(git.branch)}</span>` : ''}
      </div>
      <div class="changes-head">
        <span class="aside-label">${t('Git changes')}</span>
        ${git.root ? `<code class="mono-small">${esc(git.root)}</code>` : ''}
      </div>
      <p class="hint">${t('Read-only status and diff from')} <code>${esc(s.cwd)}</code>. ${t('No files are modified.')}</p>
      <pre class="changes-status">${esc(git.status?.trim() || t('(clean working tree)'))}${git.stat ? `\n\n${esc(git.stat)}` : ''}</pre>
      ${git.diff ? renderDiffBlock(git.diff) : `<div class="empty compact">${t('No uncommitted changes.')}</div>`}
    </div>`;
}

function inlineAttachmentLabel(name, path) {
  const raw = String(name || '').trim();
  if (raw && !/^\[image #\d+\]$/i.test(raw)) return raw;
  const clean = String(path || '').replaceAll('\\', '/');
  return (clean.split('/').filter(Boolean).pop() || 'attachment').replace(/^\d{2}-/, '');
}

function renderUserText(raw) {
  let text = String(raw || '');
  const attachments = [];
  text = text.replace(/<image\b[^>]*>/gi, (tag) => {
    const name = (tag.match(/name="([^"]*)"/i) || [])[1] || '';
    const file = (tag.match(/path="([^"]*)"/i) || [])[1] || '';
    attachments.push(inlineAttachmentLabel(name, file));
    return '';
  });
  text = text.replace(/<\/image>/gi, '');
  text = text.replace(/<(mention|skill)\b[^>]*\bname="([^"]*)"[^>]*>/gi, (_, _kind, name) => {
    attachments.push(name);
    return '';
  });
  text = text.replace(/<\/?mention[^>]*>/gi, '').replace(/<\/?skill[^>]*>/gi, '').trim();
  const chips = attachments.map((name) => `<span class="inline-attachment">${svgIcon('paperclip', 12)}${esc(name)}</span>`).join('');
  if (!text && !chips) return '';
  return `${chips}${text ? `<div class="user-text">${esc(text)}</div>` : ''}`;
}

function renderTranscriptMessage(m, agent = 'agent') {
  if (m.role === 'user' && /^\s*<(environment_context|recommended_plugins|user_instructions|permissions|skills)/i.test(m.text || '')) return '';
  if (m.kind === 'tool_call' || m.kind === 'tool_result') {
    const described = describeTool(m.tool_name || (m.kind === 'tool_result' ? 'Tool result' : 'Tool'), m.tool_input || '', m.tool_output || '');
    return historyToolCard(described, m);
  }
  if (m.role === 'user') {
    const body = renderUserText(m.text);
    if (!body) return '';
    return `<article class="turn turn-user"><div class="turn-meta"><span>${t('You')}</span><time>${esc(fmtStamp(m.timestamp))}</time></div><div class="turn-body">${body}</div></article>`;
  }
  return `<article class="turn turn-assistant"><div class="turn-meta">${agentGlyph(agent, 'small')}<span class="agent-name">${esc(agentMeta(agent).label)}</span><time>${esc(fmtStamp(m.timestamp))}</time></div><div class="turn-body md">${renderMarkdown(m.text || '')}</div></article>`;
}

async function startRun(session, fork, mode = 'terminal') {
  try {
    const run = await api(`/sessions/${session.id}/runs`, { method: 'POST', body: {
      mode, fork: Boolean(fork), cols: 120, rows: 32,
    } });
    location.hash = `#/runs/${run.id}`;
  } catch (err) {
    toast(err.message, true);
  }
}

async function pageSearch() {
  renderShell('search');
  const params = new URLSearchParams(location.hash.split('?')[1] || '');
  const initial = params.get('q') || '';
  const view = setPage(t('Search'), t('Federated transcript search runs on each online host. Content is never indexed on the hub.'));
  view.innerHTML = `
    <form class="toolbar" id="search-form">
      <input class="input grow" id="search-q" placeholder="${t('Search all transcripts, e.g. rate limiter')}" value="${esc(initial)}" autofocus>
      <select class="select" id="search-agent" aria-label="${t('Filter by agent')}"><option value="">${t('All agents')}</option></select>
      <select class="select" id="search-host" aria-label="${t('Filter by host')}"><option value="">${t('All hosts')}</option></select>
      <button class="btn primary" type="submit">${t('Search')}</button>
    </form>
    <div id="search-results">${initial ? '<div class="empty"><span class="spinner"></span></div>' : `<div class="card empty"><strong>${t('Search across hosts')}</strong>${t('Results come from the agent stores; offline hosts are reported, not silently skipped.')}</div>`}</div>`;
  let data = { results: [], not_searched: {} };
  const render = () => {
    const box = document.getElementById('search-results');
    if (!box) return;
    const agent = document.getElementById('search-agent').value;
    const host = document.getElementById('search-host').value;
    const results = data.results.filter((r) => (!agent || r.agent === agent) && (!host || r.host_name === host));
    const skipped = Object.keys(data.not_searched || {});
    box.innerHTML = `
      ${skipped.length ? `<div class="notice info mb-2">${t('Not searched:')} ${skipped.map(esc).join(', ')} ${t('(offline)')}</div>` : ''}
      ${results.length ? `<div class="list">${results.map((r) => {
        const inner = `<div style="min-width:0"><div class="title">${esc(r.title || t('Untitled session'))}</div><div class="sub"><span>${esc(r.snippet)}</span></div></div><div class="right">${agentBadge(r.agent)}${r.host_name ? `<span class="small muted">${esc(r.host_name)}</span>` : ''}<span class="small muted">${fmtRelative(r.at)}</span></div>`;
        return r.session_id
          ? `<a class="list-row" href="#/sessions/${esc(r.session_id)}">${inner}</a>`
          : `<div class="list-row" style="cursor:default">${inner}</div>`;
      }).join('')}</div>` : `<div class="card empty"><strong>${t('No matches')}</strong>${t('Try another term, another filter, or check that hosts are online.')}</div>`}`;
  };
  const run = async (query) => {
    if (query.length < 2) return;
    history.replaceState(null, '', `#/search?q=${encodeURIComponent(query)}`);
    const box = document.getElementById('search-results');
    box.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
    data = await api('/search?q=' + encodeURIComponent(query));
    const agents = [...new Set((data.results || []).map((r) => r.agent).filter(Boolean))].sort();
    const hosts = [...new Set((data.results || []).map((r) => r.host_name).filter(Boolean))].sort();
    const agentSelect = document.getElementById('search-agent');
    const hostSelect = document.getElementById('search-host');
    const agentValue = agentSelect.value;
    const hostValue = hostSelect.value;
    agentSelect.innerHTML = `<option value="">${t('All agents')}</option>` + agents.map((a) => `<option value="${esc(a)}">${esc(a)}</option>`).join('');
    hostSelect.innerHTML = `<option value="">${t('All hosts')}</option>` + hosts.map((h) => `<option value="${esc(h)}">${esc(h)}</option>`).join('');
    agentSelect.value = agents.includes(agentValue) ? agentValue : '';
    hostSelect.value = hosts.includes(hostValue) ? hostValue : '';
    render();
  };
  document.getElementById('search-agent').addEventListener('change', render);
  document.getElementById('search-host').addEventListener('change', render);
  document.getElementById('search-form').addEventListener('submit', (e) => {
    e.preventDefault();
    run(document.getElementById('search-q').value.trim()).catch((err) => toast(err.message, true));
  });
  if (initial) run(initial).catch((err) => toast(err.message, true));
}

async function pageSettings() {
  renderShell('settings');
  const view = setPage(t('Settings'), t('Appearance, account, notifications and administration.'));
  const isAdmin = state.me.role === 'admin' || state.me.role === 'owner';
  view.innerHTML = `
    <div class="grid cols-2">
      <section class="card">
        <h2 class="mt-0">${t('Appearance')}</h2>
        <p class="hint">${t('The theme is stored on the hub for your user and follows you across devices.')}</p>
        <div class="theme-choices" role="group" aria-label="${t('Theme')}">
          ${[['system', 'monitor', 'System', 'Follow the operating system'], ['dark', 'moon', 'Dark', 'Always dark'], ['light', 'sun', 'Light', 'Always light']].map(([value, icon, label, hint]) => `
            <button type="button" class="theme-choice ${state.settings.theme === value ? 'active' : ''}" data-theme-choice="${value}" aria-pressed="${state.settings.theme === value}">
              <span class="theme-icon" aria-hidden="true">${svgIcon(icon, 16)}</span>
              <span><strong>${t(label)}</strong><small>${t(hint)}</small></span>
            </button>`).join('')}
        </div>
        <label class="label mt-2">${t('Language')}<select class="select" id="settings-language">
          <option value="en" ${getLang() === 'en' ? 'selected' : ''}>English</option>
          <option value="de" ${getLang() === 'de' ? 'selected' : ''}>Deutsch</option>
        </select></label>
        <p class="hint mt-1">${t('The interface language is stored on this device. English is the default; changing it reloads the page.')}</p>
      </section>
      <section class="card">
        <h2 class="mt-0">${t('Account')}</h2>
        <div class="list">
          <div class="list-row"><div><div class="title">${esc(state.me.username)}</div><div class="sub"><span>${esc(t(state.me.role))}</span><span>${state.me.totp_enabled ? t('TOTP enabled') : t('TOTP not enabled')}</span></div></div></div>
        </div>
        <div class="split wrap mt-2">
          <a class="btn small" href="#/devices">${t('Devices & passkeys')}</a>
          <button class="btn small ghost" id="settings-signout">${t('Sign out')}</button>
        </div>
      </section>
      <section class="card">
        <h2 class="mt-0">${t('Notifications')}</h2>
        <p class="hint">${t('Content-free Web Push for approvals, completed runs, failures and host disconnects.')}</p>
        <div id="settings-push-area"><button class="btn" id="settings-push">${t('Enable on this device')}</button></div>
      </section>
      <section class="card">
        <h2 class="mt-0">${isAdmin ? t('Administration') : t('About')}</h2>
        ${isAdmin ? `<div class="split wrap"><a class="btn small" href="#/users">${t('Users & roles')}</a><a class="btn small" href="#/audit">${t('Audit log')}</a><a class="btn small" href="#/devices">${t('All devices')}</a></div>` : `<p class="hint">AgentNexus ${esc(state.hubName)} · ${t('version')} ${esc(state.version || t('unknown'))}. ${t('Sessions stay on your hosts; the hub stores metadata.')}</p>`}
      </section>
    </div>`;
  document.getElementById('settings-language').addEventListener('change', (event) => {
    setLang(event.target.value);
    location.reload();
  });
  view.querySelectorAll('[data-theme-choice]').forEach((button) => button.addEventListener('click', async () => {
    try {
      await saveTheme(button.dataset.themeChoice);
      view.querySelectorAll('[data-theme-choice]').forEach((other) => {
        const active = other.dataset.themeChoice === button.dataset.themeChoice;
        other.classList.toggle('active', active);
        other.setAttribute('aria-pressed', String(active));
      });
    } catch (err) { toast(err.message, true); }
  }));
  document.getElementById('settings-signout').addEventListener('click', logout);
  document.getElementById('settings-push')?.addEventListener('click', async () => {
    try {
      await enablePush();
      document.getElementById('settings-push-area').innerHTML = badge(t('enabled on this device'), 'online');
      toast(t('Push notifications enabled'));
    } catch (err) { toast(err.message, true); }
  });
}

async function pageDevices() {
  renderShell('devices');
  const view = setPage(t('Devices'), t('Signed-in browsers. Revoke anything you no longer trust; revocation ends its session immediately.'), `<button class="btn primary small" id="pair-device">${t('Pair a device')}</button>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const data = await api('/devices');
  view.innerHTML = `
    <div class="table-wrap">
      <table class="devices-table"><thead><tr><th>${t('Device')}</th><th>${t('Created')}</th><th>${t('Last seen')}</th><th></th></tr></thead><tbody>
      ${data.devices.map((d) => `<tr>
        <td><b>${esc(d.name)}</b><div class="mono-small">${esc(shortId(d.id))}</div></td>
        <td>${esc(fmtTime(d.created_at))}</td>
        <td>${esc(fmtTime(d.last_seen))}</td>
        <td class="nowrap">${d.id === data.current_device_id ? badge('this device', 'online') : `<button class="btn small danger" data-revoke-device="${esc(d.id)}">${t('Revoke')}</button>`}</td>
      </tr>`).join('')}
      </tbody></table>
    </div>
    <div class="card mt-3">
      <h2 class="mt-0">${t('Authenticator (TOTP)')}</h2>
      <p class="hint">${state.me.totp_enabled ? t('TOTP is enabled. A code is required at every login.') : t('TOTP is not enabled yet. Enrolling it adds a second factor to password login.')}</p>
      <div id="totp-area">
        ${state.me.totp_enabled ? badge(t('enabled'), 'online') : `<button class="btn" id="totp-begin">${t('Set up authenticator')}</button>`}
      </div>
    </div>
    <div class="card mt-2">
      <h2 class="mt-0">${t('Passkeys')}</h2>
      <p class="hint">${t('Passkeys use WebAuthn and are phishing-resistant. Add one to this device, then use “Sign in with a passkey” on the login page.')}</p>
      <div id="passkey-area"><button class="btn" id="add-passkey">${t('Add a passkey')}</button></div>
    </div>
    <div class="card mt-2">
      <h2 class="mt-0">${t('Web Push notifications')}</h2>
      <p class="hint">${t('Content-free alerts for run completion and approval requests. The payload never contains transcript text, commands or file paths.')}</p>
      <div id="push-area"><button class="btn" id="enable-push">${t('Enable notifications')}</button></div>
    </div>
    <p class="small muted mt-2">${t('Passkey login and Argon2id passwords with optional TOTP are available. Session cookies are HttpOnly, SameSite=Strict and Secure when TLS is configured.')}</p>`;
  view.querySelectorAll('[data-revoke-device]').forEach((b) => b.addEventListener('click', () => {
    confirmDanger('Revoking this device ends all of its signed-in sessions.', async () => {
      await api(`/devices/${b.dataset.revokeDevice}`, { method: 'DELETE' });
      toast(t('Device revoked'));
      pageDevices();
    });
  }));
  document.getElementById('pair-device')?.addEventListener('click', async () => {
    try {
      const data = await api('/devices/pairing', { method: 'POST' });
      modal(`
        <h2>${t('Pair a device')}</h2>
        <p class="hint">${t('Scan this code on a new device. It is single-use and expires in two minutes. The new device gets its own revocable session.')}</p>
        <div style="text-align:center"><img src="${data.qr}" alt="Device pairing QR code" style="width:min(320px,100%);border-radius:12px;background:#fff;padding:8px"></div>
        <div class="secret mt-2">${esc(data.url)}</div>
        <div class="modal-actions">
          <button class="btn" type="button" id="copy-pair">${t('Copy link')}</button>
          <button class="btn primary" type="button" data-close>${t('Done')}</button>
        </div>`);
      document.getElementById('copy-pair').addEventListener('click', () => copyText(data.url));
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('add-passkey')?.addEventListener('click', async () => {
    try {
      await registerPasskey();
      document.getElementById('passkey-area').innerHTML = badge(t('passkey added'), 'online');
      toast(t('Passkey added'));
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('enable-push')?.addEventListener('click', async () => {
    try {
      await enablePush();
      document.getElementById('push-area').innerHTML = badge('enabled on this device', 'online');
      toast('Push notifications enabled');
    } catch (err) {
      toast(err.message, true);
    }
  });
  document.getElementById('totp-begin')?.addEventListener('click', async () => {
    try {
      const data = await api('/auth/totp/begin', { method: 'POST' });
      modal(`
        <h2>${t('Set up authenticator')}</h2>
        <p class="hint">${t('Add this secret to your authenticator app, then enter the current six-digit code to confirm.')}</p>
        <div class="secret">${esc(data.secret)}</div>
        <div class="secret mt-1">${esc(data.url)}</div>
        <form class="form mt-2" id="totp-form">
          <label class="label">${t('Current code')}<input class="input" name="code" inputmode="numeric" maxlength="6" pattern="[0-9]{6}" required></label>
          <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Cancel')}</button><button class="btn primary" type="submit">${t('Enable TOTP')}</button></div>
        </form>`);
      document.getElementById('totp-form').addEventListener('submit', async (e) => {
        e.preventDefault();
        try {
          await api('/auth/totp/enable', { method: 'POST', body: { secret: data.secret, code: new FormData(e.target).get('code') } });
          closeModal();
          state.me = await api('/me');
          toast(t('TOTP enabled'));
          pageDevices();
        } catch (err) { toast(err.message, true); }
      });
    } catch (err) { toast(err.message, true); }
  });
}

async function pageUsers() {
  renderShell('users');
  const view = setPage(t('Users'), t('Hub roles and per-host permissions. Per-host grants are the unit of least privilege; host policy is still enforced separately.'));
  const data = await api('/users');
  const isOwner = state.me.role === 'owner';
  view.innerHTML = `
    <div class="table-wrap">
      <table><thead><tr><th>${t('User')}</th><th>${t('Role')}</th><th>${t('Created')}</th></tr></thead><tbody>
      ${data.users.map((u) => `<tr>
        <td><b>${esc(u.username)}</b>${u.id === state.me.id ? ` <span class="badge online">${t('you')}</span>` : ''}<div class="mono-small">${esc(shortId(u.id))}</div></td>
        <td>${isOwner && u.id !== state.me.id ? `<select class="select" data-role-user="${esc(u.id)}">${['admin','member','viewer'].map((r) => `<option ${u.role === r ? 'selected' : ''}>${r}</option>`).join('')}</select>` : esc(t(u.role) || '—')}</td>
        <td>${esc(fmtTime(u.created_at))}</td>
      </tr>`).join('')}</tbody></table>
    </div>
    ${isOwner ? `<div class="card mt-3">
      <h2 class="mt-0">${t('Add a user')}</h2>
      <p class="hint">${t('The new user signs in with this password and can change roles only through an owner. TOTP can be enrolled after their first login.')}</p>
      <form class="form" id="create-user-form">
        <div class="form-row">
          <label class="label">${t('Username')}<input class="input" name="username" required minlength="3"></label>
          <label class="label">${t('Password')}<input class="input" name="password" type="password" required minlength="10"></label>
          <label class="label">${t('Role')}<select class="select" name="role"><option value="member">${t('member')}</option><option value="viewer">${t('viewer')}</option><option value="admin">${t('admin')}</option></select></label>
        </div>
        <div><button class="btn primary" type="submit">${t('Create user')}</button></div>
      </form>
    </div>` : ''}`;
  view.querySelectorAll('[data-role-user]').forEach((sel) => sel.addEventListener('change', async () => {
    try {
      await api(`/users/${sel.dataset.roleUser}`, { method: 'PATCH', body: { role: sel.value } });
      toast(t('Role updated'));
    } catch (err) { toast(err.message, true); pageUsers(); }
  }));
  document.getElementById('create-user-form')?.addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = new FormData(event.target);
    try {
      await api('/users', { method: 'POST', body: {
        username: form.get('username'), password: form.get('password'), role: form.get('role'),
      } });
      toast(t('User created'));
      pageUsers();
    } catch (err) { toast(err.message, true); }
  });
}

async function shareHostDialog(host) {
  try {
    await ensureStepUp();
  } catch {
    return;
  }
  const [users, grants] = await Promise.all([api('/users'), api(`/hosts/${host.id}/grants`)]);
  const byUser = new Map(grants.grants.map((g) => [g.user_id, g.permissions.split(',')]));
  const permissions = ['view','read_transcript','resume','create','fork','terminal_input','approve','manage'];
  modal(`
    <h2>${t('Share')} ${esc(host.name)}</h2>
    <p class="hint">${t('The hub grant is only one gate; the host policy still decides which agents, roots and actions are allowed.')}</p>
    <div class="form">
      <label class="label">${t('User')}<select class="select" id="share-user">${users.users.map((u) => `<option value="${esc(u.id)}">${esc(u.username)}${u.id === state.me.id ? ' (you)' : ''}</option>`).join('')}</select></label>
      <div class="grid cols-2">${permissions.map((p) => `<label class="split small"><input type="checkbox" data-perm="${p}"> ${p.replaceAll('_',' ')}</label>`).join('')}</div>
      <div class="modal-actions">
        <button class="btn danger" type="button" id="remove-grant">${t('Remove access')}</button>
        <button class="btn" type="button" data-close>${t('Cancel')}</button>
        <button class="btn primary" type="button" id="save-grant">${t('Save grant')}</button>
      </div>
    </div>`);
  const select = document.getElementById('share-user');
  const setChecks = () => {
    const current = byUser.get(select.value) || [];
    document.querySelectorAll('[data-perm]').forEach((cb) => { cb.checked = current.includes(cb.dataset.perm); });
  };
  select.addEventListener('change', setChecks);
  setChecks();
  document.getElementById('save-grant').addEventListener('click', async () => {
    const selected = [...document.querySelectorAll('[data-perm]:checked')].map((cb) => cb.dataset.perm);
    try {
      await api(`/hosts/${host.id}/grants/${select.value}`, { method: 'PUT', body: { permissions: selected } });
      toast(t('Grant saved'));
      closeModal();
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('remove-grant').addEventListener('click', async () => {
    try {
      await api(`/hosts/${host.id}/grants/${select.value}`, { method: 'DELETE' });
      toast('Grant removed');
      closeModal();
    } catch (err) { toast(err.message, true); }
  });
}

async function pageAudit() {
  renderShell('audit');
  const view = setPage(t('Audit log'), t('Append-only, hash-chained security events stored on the hub.'));
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const data = await api('/audit?limit=300');
  view.innerHTML = `
    ${data.broken_seq ? `<div class="notice error mb-2">${t('Audit chain broken at sequence')} ${esc(data.broken_seq)}. ${t('Investigate the database immediately.')}</div>` : `<div class="notice info mb-2">${t('Hash chain verified: no broken link in the checked range.')}</div>`}
    <div class="list">${data.events.map((e) => `
      <div class="audit-row">
        <div class="split wrap"><span class="badge no-dot">#${esc(e.seq)}</span><strong>${esc(e.action)}</strong><span class="right small muted">${esc(fmtTime(e.at))} · ${esc(e.actor)}</span></div>
        ${e.details && e.details !== 'null' ? `<pre class="audit-details">${esc(e.details)}</pre>` : ''}
      </div>`).join('')}</div>`;
}

function toolIcon(kind) {
  return { terminal: 'terminal', read: 'file', edit: 'edit', write: 'edit', search: 'searchFile', web: 'web', git: 'git', test: 'check', todo: 'plan', task: 'task', mcp: 'mcp', image: 'image' }[kind] || 'tool';
}

function renderDiffBlock(text, title = 'diff') {
  const files = parseUnifiedDiff(text);
  if (!files.length) {
    return `<div class="md-code diff-view"><div class="md-code-head"><span class="md-code-lang">diff</span><button class="md-copy" type="button">${t('Copy')}</button></div><pre><code>${highlightCode(String(text || ''), 'diff')}</code></pre></div>`;
  }
  const summary = diffSummary(files);
  const mode = storeGet('nexus-diff-mode', 'unified');
  return `<div class="diff-panel" data-mode="${mode}">
    <div class="diff-toolbar">
      <span class="diff-summary">${countLabel(summary.files, 'file', 'files')} <b>+${summary.additions}</b> <i>-${summary.deletions}</i></span>
      <div class="diff-mode-switch" role="group" aria-label="${t('Diff layout')}">
        <button type="button" data-diff-mode="unified" class="${mode === 'unified' ? 'active' : ''}">${t('Unified')}</button>
        <button type="button" data-diff-mode="split" class="${mode === 'split' ? 'active' : ''}">${t('Split')}</button>
      </div>
    </div>
    <div class="diff-file-list">${diffFileList(files)}</div>
    <div class="diff-body">
      <div class="diff-unified">${renderUnifiedDiff(files, { title })}</div>
      <div class="diff-split">${renderSplitDiff(files)}</div>
    </div>
  </div>`;
}

function applyDiffMode(panel, mode) {
  if (!panel) return;
  panel.dataset.mode = mode;
  storeSet('nexus-diff-mode', mode);
  panel.querySelectorAll('[data-diff-mode]').forEach((button) => {
    button.classList.toggle('active', button.dataset.diffMode === mode);
  });
}

function outputBlock(text) {
  const value = String(text || '');
  if (!value) return '';
  if (/^(diff --git|@@ )/m.test(value) && /^[+-]/m.test(value)) return renderDiffBlock(value);
  return `<pre class="tool-output">${esc(value)}</pre>`;
}

function toolCardHtml(described, status = 'done') {
  const detail = described.detail ? `<code>${esc(described.detail)}</code>` : '';
  const statusMark = status === 'running'
    ? `<span class="spinner tiny"></span>${t('Running')}`
    : status === 'error'
      ? `${svgIcon('alert', 13)}${t('Failed')}`
      : `${svgIcon('check', 13)}${t('Done')}`;
  const commandBlock = described.command && described.kind === 'terminal'
    ? `<div class="tool-block"><div class="tool-label">${t('Command')}</div><pre class="tool-pre">${esc(described.command)}</pre></div>`
    : '';
  const pathBlock = described.path && described.kind !== 'terminal' && described.kind !== 'edit' && described.kind !== 'write'
    ? `<div class="tool-block"><div class="tool-label">${t('Path')}</div><pre class="tool-pre">${esc(described.path)}</pre></div>`
    : '';
  const diffBlock = described.diff ? renderDiffBlock(described.diff) : '';
  const output = described.kind !== 'edit' && described.output ? `<div class="tool-block"><div class="tool-label">${t('Output')}</div>${outputBlock(described.output)}</div>` : '';
  const hasStructured = commandBlock || pathBlock || diffBlock || output;
  const raw = described.inputText && described.inputText !== '{}' && described.inputText !== 'null'
    ? `<details class="tool-raw"><summary>${t('Raw input')}</summary><pre>${esc(described.inputText)}</pre></details>`
    : '';
  return `<details class="tool-card ${status}" ${status === 'error' ? 'open' : ''}>
    <summary>
      <span class="tool-icon">${svgIcon(toolIcon(described.kind), 15)}</span>
      <span class="tool-line"><strong>${esc(described.label)}</strong>${detail}</span>
      <span class="tool-state ${status}">${statusMark}</span>
      <span class="tool-chevron">${svgIcon('chevronDown', 14)}</span>
    </summary>
    <div class="tool-body">${hasStructured ? commandBlock + pathBlock + diffBlock + output : `<div class="tool-empty">${t('No further details.')}</div>`}${raw}</div>
  </details>`;
}

function historyToolCard(described, message) {
  const status = message.kind === 'tool_result' && /(^|\n)(error|failed|exit code [1-9])/i.test(message.tool_output || '') ? 'error' : 'done';
  return toolCardHtml(described, status);
}

function reasoningHtml(text) {
  return `<details class="reasoning-card"><summary>${svgIcon('brain', 14)}<span>${t('Thinking')}</span><span class="reasoning-toggle">${svgIcon('chevronDown', 13)}</span></summary><div class="reasoning-body md">${renderMarkdown(text)}</div></details>`;
}

function workingHtml() {
  return `<div class="working-indicator" id="working-indicator"><span class="working-dots"><i></i><i></i><i></i></span><span>${t('Agent is working')}</span></div>`;
}

function renderTranscriptMessages(messages, agent = 'agent') {
  const html = [];
  for (let i = 0; i < messages.length; i += 1) {
    const message = messages[i];
    if (message.kind === 'tool_call') {
      const next = messages[i + 1];
      const output = next?.kind === 'tool_result' ? (next.tool_output || '') : '';
      if (next?.kind === 'tool_result') i += 1;
      const described = describeTool(message.tool_name || 'Tool', message.tool_input || '', output);
      html.push(historyToolCard(described, { ...message, tool_output: output }));
    } else if (message.kind === 'tool_result') {
      const described = describeTool(message.tool_name || 'Tool result', '', message.tool_output || '');
      html.push(historyToolCard(described, message));
    } else {
      html.push(renderTranscriptMessage(message, agent));
    }
  }
  return html.join('');
}

async function pageStructuredRun(id, run) {
  let session = null;
  if (run.session_id) {
    session = await api(`/sessions/${encodeURIComponent(run.session_id)}`).catch(() => null);
  }
  const agent = agentMeta(run.agent || session?.agent);
  const title = session?.title || run.session_title || t('Structured chat');
  const cwd = session?.cwd || run.cwd || '';
  const branch = session?.git_branch || run.git_branch || '';
  const host = run.host_name || session?.host_name || run.host_id;
  const contextDefault = !state.embed && storeGet('nexus-context-open', window.innerWidth >= 1180);
  setPage(title, `${projectName(cwd)} · ${agent.label} · ${host}`, `
    <button class="btn small ghost icon-mobile" id="chat-context-toggle" title="${t('Toggle session details')}" aria-label="${t('Toggle session details')}">${svgIcon('panelRight')} <span>${t('Details')}</span></button>
    <button class="btn small danger icon-mobile" id="stop-run" title="${t('Stop this run')}" aria-label="${t('Stop this run')}">${svgIcon('stop')} <span>${t('Stop')}</span></button>
    <a class="btn small ghost icon-mobile" href="#/workspace?pane=${encodeURIComponent(id)}" title="${t('Open split workspace')}" aria-label="${t('Open split workspace')}">${svgIcon('panelRight')} <span>${t('Split')}</span></a>
    <a class="btn small ghost icon-mobile" href="#/sessions" title="${t('All sessions')}" aria-label="${t('All sessions')}">${svgIcon('sessions')} <span>${t('Sessions')}</span></a>`);
  openTab({ id, key: session?.id || id, kind: 'run', href: `#/runs/${id}`, title, agent: run.agent || session?.agent });
  rememberRecent({ id, title, agent: run.agent || session?.agent, hint: `${agent.label} · ${host} · ${projectName(cwd)}`, href: `#/runs/${id}` });
  const view = document.getElementById('view');
  view.className = 'view chat-view';
  view.innerHTML = `
    <div class="chat-page ${contextDefault ? 'context-open' : ''}" id="chat-page">
      <section class="chat-main">
        <div class="chat-statusbar">
          <span class="badge running" id="chat-state">${esc(t((run.state || 'starting').replaceAll('_', ' ')))}</span>
          <span class="chat-agent">${agentGlyph(run.agent || session?.agent)}${esc(agent.label)}</span>
          <span class="chat-sep">·</span>
          <span class="chat-host">${esc(host)}</span>
          ${branch ? `<span class="branch-chip">${svgIcon('branch', 12)}${esc(branch)}</span>` : ''}
          <button class="icon-btn chat-inline-context" id="chat-inline-context" type="button" title="${t('Toggle session details')}" aria-label="${t('Toggle session details')}">${svgIcon('panelRight')}</button>
          <span class="chat-connection" id="run-note"><span class="connection-dot"></span>Connecting…</span>
        </div>
        <div class="chat-scroll" id="chat">
          <div class="chat-inner">
            <div id="history-top"></div>
            <div id="messages" class="messages"></div>
            <div id="working-slot"></div>
          </div>
        </div>
        <div class="composer-wrap">
          <button class="jump-latest" id="jump-latest" type="button" hidden>${svgIcon('arrowDown', 14)} ${t('Jump to latest')}</button>
          <div id="approval-banner" class="approval-slot"></div>
          <form class="composer" id="prompt-form">
            <textarea id="prompt-text" rows="1" placeholder="${t('Message {agent}…', { agent: agent.label })}" enterkeyhint="send" autocomplete="off" spellcheck="false"></textarea>
            <div class="composer-config" id="composer-config"></div>
            <div class="attachment-list" id="attachment-list"></div>
            <div class="composer-bar">
              <div class="composer-info">${agentGlyph(run.agent || session?.agent, 'small')}<span>${esc(agent.label)}</span><span class="composer-pill">${esc(run.mode)}</span><span class="composer-pill host-pill">${esc(host)}</span></div>
              <div class="composer-actions">
                <span class="composer-hint">${t('Enter to send · Shift+Enter newline')}</span>
                <input type="file" id="attach-input" multiple hidden>
                <button class="icon-btn" id="attach-button" type="button" title="${t('Attach files')}" aria-label="${t('Attach files')}" hidden>${svgIcon('paperclip')}</button>
                <button class="icon-btn composer-stop" id="interrupt-run" type="button" title="${t('Interrupt current turn')}" aria-label="${t('Interrupt current turn')}" hidden>${svgIcon('stop')}</button>
                <button class="send-button" id="send-prompt" type="submit" title="${t('Send message')}" aria-label="${t('Send message')}">${svgIcon('send')}</button>
              </div>
            </div>
          </form>
        </div>
      </section>
      <aside class="chat-context" id="chat-context">
        <div class="context-head"><strong>${t('Session')}</strong><button class="icon-btn" id="context-close" type="button" aria-label="${t('Close details')}">${svgIcon('x')}</button></div>
        <div class="context-body">
          <section class="context-section">
            <div class="aside-label">${t('Run')}</div>
            <div class="meta-list">
              <div><span>${t('State')}</span><strong id="context-state">${esc(t((run.state || 'starting').replaceAll('_', ' ')))}</strong></div>
              <div><span>${t('Agent')}</span><strong>${esc(agent.label)}</strong></div>
              <div><span>${t('Mode')}</span><strong>${esc(run.mode)}</strong></div>
              <div><span>${t('Host')}</span><strong>${esc(host)}</strong></div>
              <div><span>${t('Started')}</span><strong>${esc(fmtTime(run.started_at))}</strong></div>
            </div>
          </section>
          <section class="context-section">
            <div class="aside-label">${t('Native session')}</div>
            ${session ? `<div class="meta-list">
              <div><span>${t('Messages')}</span><strong>${session.message_count || 0}</strong></div>
              <div><span>${t('Updated')}</span><strong>${esc(fmtRelative(session.updated_at))}</strong></div>
            </div>
            <div class="copy-line"><code>${esc(session.native_id || session.id)}</code><button class="icon-btn" data-copy="${esc(session.native_id || session.id)}" title="${t('Copy native id')}">${svgIcon('copy', 14)}</button></div>
            <div class="copy-line"><code>${esc(cwd)}</code><button class="icon-btn" data-copy="${esc(cwd)}" title="${t('Copy working directory')}">${svgIcon('copy', 14)}</button></div>
            ${session.repo_url ? `<div class="aside-repo">${repoLink(session.repo_url)}${session.repo_root && cwdKey(session.repo_root) !== cwdKey(cwd) ? '<span class="muted">worktree</span>' : ''}</div>` : ''}` : `<p class="aside-note">${t('This run was started without a discovered native session.')}</p>`}
          </section>
          <section class="context-section">
            <div class="aside-label">${t('Git')}</div>
            <div id="context-git"><button class="btn small block" id="context-git-load">${t('Load working tree changes')}</button></div>
          </section>
          <section class="context-section">
            <div class="aside-label">${t('Actions')}</div>
            <div class="aside-actions">
              ${session ? `<button class="btn small" id="context-pin">${svgIcon('star')} ${state.favorites.has(session.id) ? t('Unpin session') : t('Pin session')}</button><a class="btn small" href="#/sessions/${esc(session.id)}">${svgIcon('file')} ${t('Read-only transcript')}</a><button class="btn small" id="context-terminal">${svgIcon('terminal')} ${t('Terminal mode')}</button><a class="btn small" href="/api/v1/sessions/${esc(session.id)}/export?format=markdown">${t('Export Markdown')}</a>` : ''}
            </div>
          </section>
          <section class="context-section">
            <div class="aside-label">${t('Continuity')}</div>
            <p class="aside-note">${t("This chat drives the agent's native session. You can return to the same session in the original CLI, and the transcript stays on the host.")}</p>
          </section>
        </div>
      </aside>
      <div class="context-resizer" id="context-resizer" title="${t('Drag to resize details')}"></div>
      <div class="chat-scrim" id="chat-scrim"></div>
    </div>`;

  const chat = document.getElementById('chat');
  const messagesEl = document.getElementById('messages');
  messagesEl.dataset.empty = t('Send a message to continue this native session.');
  const historyTop = document.getElementById('history-top');
  const workingSlot = document.getElementById('working-slot');
  const stateNode = document.getElementById('chat-state');
  const contextState = document.getElementById('context-state');
  const note = document.getElementById('run-note');
  const approval = document.getElementById('approval-banner');
  const form = document.getElementById('prompt-form');
  const textarea = document.getElementById('prompt-text');
  const sendButton = document.getElementById('send-prompt');
  const stopButton = document.getElementById('interrupt-run');
  const jumpButton = document.getElementById('jump-latest');
  const chatPage = document.getElementById('chat-page');
  const contextWidth = Math.min(520, Math.max(240, Number(storeGet('nexus-context-width', 320)) || 320));
  chatPage.style.setProperty('--context-width', `${contextWidth}px`);
  const resizer = document.getElementById('context-resizer');
  let resizing = false;
  resizer.addEventListener('pointerdown', (event) => {
    if (window.innerWidth <= 1100) return;
    resizing = true;
    resizer.setPointerCapture(event.pointerId);
    document.body.style.userSelect = 'none';
    document.body.style.cursor = 'col-resize';
  });
  resizer.addEventListener('pointermove', (event) => {
    if (!resizing) return;
    const width = Math.min(520, Math.max(240, window.innerWidth - event.clientX));
    chatPage.style.setProperty('--context-width', `${width}px`);
  });
  const endResize = () => {
    if (!resizing) return;
    resizing = false;
    document.body.style.userSelect = '';
    document.body.style.cursor = '';
    const width = Number.parseInt(getComputedStyle(chatPage).getPropertyValue('--context-width'), 10);
    if (Number.isFinite(width)) storeSet('nexus-context-width', width);
  };
  resizer.addEventListener('pointerup', endResize);
  resizer.addEventListener('pointercancel', endResize);
  const configBar = document.getElementById('composer-config');
  let runConfigOptions = null;
  const configKey = `nexus-run-config-${id}`;
  const readConfig = () => storeGet(configKey, {});
  const writeConfig = (patch) => storeSet(configKey, { ...readConfig(), ...patch });
  let selectedModelIndex = -1;
  const modelKey = (m) => `${m?.provider || 'agent'}|${m?.id || ''}`;
  const modelList = (key) => {
    const value = storeGet(key, []);
    return Array.isArray(value) ? value.filter((item) => typeof item === 'string') : [];
  };
  const rememberModel = (key) => {
    const recent = modelList('nexus-model-recent').filter((item) => item !== key);
    recent.unshift(key);
    storeSet('nexus-model-recent', recent.slice(0, 8));
  };
  const fuzzyMatch = (query, text) => {
    const q = query.trim().toLowerCase();
    if (!q) return true;
    const haystack = text.toLowerCase();
    if (haystack.includes(q)) return true;
    let index = 0;
    for (const ch of haystack) {
      if (ch === q[index]) index += 1;
      if (index === q.length) return true;
    }
    return index === q.length;
  };
  const selectedModel = () => {
    if (!runConfigOptions?.models?.length || selectedModelIndex < 0) return null;
    return runConfigOptions.models[selectedModelIndex] || null;
  };
  const openModelPicker = () => {
    const models = runConfigOptions?.models || [];
    if (!models.length) return;
    const byKey = new Map(models.map((model, index) => [modelKey(model), { model, index }]));
    let query = '';
    modal(`
      <div class="model-picker-head"><h2>${t('Choose a model')}</h2><span class="muted">${t('{n} available', { n: models.length })}</span></div>
      <label class="model-search">${svgIcon('search', 14)}<input id="model-search" type="search" placeholder="${t('Search model or provider?')}" autocomplete="off" spellcheck="false" autofocus></label>
      <div class="model-picker-list" id="model-picker-list"></div>
      <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Close')}</button></div>`);
    const list = document.getElementById('model-picker-list');
    const search = document.getElementById('model-search');
    const renderList = () => {
      const favorites = new Set(modelList('nexus-model-favorites'));
      const recent = modelList('nexus-model-recent');
      let entries = models.map((model, index) => ({ model, index }));
      if (query.trim()) {
        entries = entries.filter(({ model }) => fuzzyMatch(query, `${model.name || ''} ${model.id || ''} ${model.provider || ''}`));
      } else {
        const rank = (model) => {
          const key = modelKey(model);
          const recentIndex = recent.indexOf(key);
          return recentIndex >= 0 ? recentIndex : (favorites.has(key) ? 100 : 1000);
        };
        entries.sort((a, b) => rank(a.model) - rank(b.model));
      }
      list.innerHTML = entries.length ? entries.map(({ model, index }) => {
        const key = modelKey(model);
        const active = index === selectedModelIndex;
        const variant = (model.variants || []).length ? t('{n} reasoning levels', { n: model.variants.length }) : t('default reasoning');
        return `<div class="model-row ${active ? 'active' : ''}">
          <button class="model-main" type="button" data-model-pick="${esc(key)}" data-model-index="${index}">
            <span class="model-name">${esc(model.name || model.id)}</span>
            <span class="model-meta">${esc(model.provider || 'agent')} &middot; ${esc(variant)}</span>
          </button>
          <button class="model-star ${favorites.has(key) ? 'active' : ''}" type="button" data-model-star="${esc(key)}" aria-label="${favorites.has(key) ? t('Remove favorite') : t('Add favorite')}" aria-pressed="${favorites.has(key) ? 'true' : 'false'}">${svgIcon('star', 14)}</button>
        </div>`;
      }).join('') : `<div class="empty compact">${t('No models match that search.')}</div>`;
    };
    renderList();
    search.addEventListener('input', () => { query = search.value; renderList(); });
    list.addEventListener('click', (event) => {
      const star = event.target.closest('[data-model-star]');
      if (star) {
        const key = star.dataset.modelStar;
        const favorites = modelList('nexus-model-favorites');
        const next = favorites.includes(key) ? favorites.filter((item) => item !== key) : [key, ...favorites];
        storeSet('nexus-model-favorites', next);
        renderList();
        return;
      }
      const pick = event.target.closest('[data-model-pick]');
      if (!pick) return;
      selectedModelIndex = Number(pick.dataset.modelIndex);
      const model = models[selectedModelIndex];
      if (!model) return;
      writeConfig({ model: model.id, provider: model.provider || '', variant: '' });
      rememberModel(modelKey(model));
      closeModal();
      renderComposerConfig();
    });
    requestAnimationFrame(() => search.focus());
  };
  const renderComposerConfig = () => {
    if (!configBar || !runConfigOptions) return;
    const models = runConfigOptions.models || [];
    const modes = runConfigOptions.modes || [];
    if (!models.length && !modes.length) { configBar.innerHTML = ''; configBar.hidden = true; return; }
    const saved = readConfig();
    selectedModelIndex = models.findIndex((m) => m.id === saved.model && (!saved.provider || m.provider === saved.provider));
    if (selectedModelIndex < 0) selectedModelIndex = models.findIndex((m) => m.id === runConfigOptions.current_model && (!runConfigOptions.current_provider || m.provider === runConfigOptions.current_provider));
    if (selectedModelIndex < 0) selectedModelIndex = 0;
    const model = models[selectedModelIndex];
    const variants = model?.variants || [];
    let variant = saved.variant || '';
    if (variant && !variants.includes(variant)) variant = '';
    if (!variant && model?.default_variant && variants.includes(model.default_variant)) variant = model.default_variant;
    if (!variant && model && variants.length && runConfigOptions.current_model === model.id) variant = runConfigOptions.current_variant || '';
    const mode = saved.agent_mode || runConfigOptions.current_mode || '';
    configBar.hidden = false;
    configBar.innerHTML = `
      ${models.length ? `<div class="composer-field"><span>${t('Model')}</span><button class="composer-model" id="opt-model-btn" type="button" title="${t('Switch model')}"><span class="composer-model-name">${esc(model?.name || model?.id || t('Select model'))}</span><span class="composer-model-provider">${esc(model?.provider || '')}</span>${svgIcon('chevronDown', 12)}</button></div>` : ''}
      ${variants.length ? `<label class="composer-field"><span>${t('Reasoning')}</span><select id="opt-variant" class="composer-select"><option value="">${t('Default')}</option>${variants.map((v) => `<option value="${esc(v)}" ${v === variant ? 'selected' : ''}>${esc(v)}</option>`).join('')}</select></label>` : ''}
      ${modes.length ? `<label class="composer-field"><span>${t('Agent')}</span><select id="opt-agent" class="composer-select"><option value="">${t('Default')}</option>${modes.map((m) => `<option value="${esc(m.id)}" ${m.id === mode ? 'selected' : ''}>${esc(m.name || m.id)}</option>`).join('')}</select></label>` : ''}`;
    document.getElementById('opt-model-btn')?.addEventListener('click', openModelPicker);
    document.getElementById('opt-variant')?.addEventListener('change', (event) => writeConfig({ variant: event.target.value }));
    document.getElementById('opt-agent')?.addEventListener('change', (event) => writeConfig({ agent_mode: event.target.value }));
    renderAttachments();
  };
  const currentPromptOptions = () => {
    // Only send a setting the user explicitly changed. Re-sending the current
    // native values on every prompt would rewrite agent configuration the user
    // never touched and can fail on models that do not accept a variant.
    const saved = readConfig();
    const model = selectedModel();
    const variant = document.getElementById('opt-variant')?.value || '';
    const agentMode = document.getElementById('opt-agent')?.value || '';
    const options = {};
    if (model && (saved.model || saved.variant)) {
      options.model = model.id;
      options.provider = model.provider || '';
      if (variant) options.variant = variant;
    }
    if (agentMode && saved.agent_mode) options.agent_mode = agentMode;
    if (pendingAttachments.length) {
      options.attachments = pendingAttachments.map((file) => ({ name: file.name, mime: file.mime, data: file.data }));
    }
    return options;
  };
  let optionsRetries = 0;
  const loadRunOptions = async () => {
    try {
      runConfigOptions = await api(`/runs/${encodeURIComponent(id)}/options`);
      renderComposerConfig();
      const empty = !(runConfigOptions.models?.length || runConfigOptions.modes?.length);
      if (empty && optionsRetries < 8) {
        optionsRetries += 1;
        setTimeout(() => { loadRunOptions().catch(() => {}); }, 1500);
      }
    } catch {
      runConfigOptions = null;
      if (optionsRetries < 8) {
        optionsRetries += 1;
        setTimeout(() => { loadRunOptions().catch(() => {}); }, 1500);
      }
    }
  };
  const attachmentList = document.getElementById('attachment-list');
  const attachButton = document.getElementById('attach-button');
  const attachInput = document.getElementById('attach-input');
  let pendingAttachments = [];
  const attachmentKinds = () => selectedModel()?.inputs || [];
  const allowedAttachmentKinds = () => attachmentKinds().map((kind) => kind.toLowerCase()).filter((kind) => ['image', 'file', 'pdf', 'audio', 'text'].includes(kind));
  const attachmentMatches = (file, kinds) => {
    const type = String(file.type || file.mime || '').toLowerCase();
    return !kinds.length || kinds.includes('file') || kinds.some((kind) => (
      kind === 'pdf' ? type === 'application/pdf' : type.startsWith(kind + '/')
    ));
  };
  const attachmentAllowed = () => allowedAttachmentKinds().length > 0;
  const attachmentAccept = () => {
    const kinds = attachmentKinds().map((kind) => kind.toLowerCase());
    const accepts = [];
    if (kinds.includes('image')) accepts.push('image/*');
    if (kinds.includes('audio')) accepts.push('audio/*');
    if (kinds.includes('pdf')) accepts.push('application/pdf');
    if (kinds.includes('text')) accepts.push('text/*');
    return accepts.join(',') || undefined;
  };
  const renderAttachments = () => {
    if (!attachmentList || !attachButton) return;
    const kinds = allowedAttachmentKinds();
    const allowed = kinds.length > 0;
    attachButton.hidden = !allowed;
    if (attachInput) attachInput.accept = attachmentAccept() || '';
    if (!allowed) pendingAttachments = [];
    else pendingAttachments = pendingAttachments.filter((file) => attachmentMatches(file, kinds));
    attachmentList.innerHTML = pendingAttachments.map((file, index) => `
      <span class="attachment-chip" title="${esc(file.name)}">
        ${file.preview ? `<img src="${file.preview}" alt="">` : svgIcon('file', 13)}
        <span class="attachment-name">${esc(file.name)}</span>
        <small>${Math.max(1, Math.round(file.size / 1024))} KB</small>
        <button class="attachment-remove" type="button" data-remove-attachment="${index}" aria-label="Remove ${esc(file.name)}">${svgIcon('x', 12)}</button>
      </span>`).join('');
    attachmentList.querySelectorAll('[data-remove-attachment]').forEach((button) => button.addEventListener('click', () => {
      pendingAttachments.splice(Number(button.dataset.removeAttachment), 1);
      renderAttachments();
    }));
  };
  attachButton?.addEventListener('click', () => attachInput?.click());
  attachInput?.addEventListener('change', async () => {
    const files = [...(attachInput.files || [])];
    attachInput.value = '';
    const kinds = allowedAttachmentKinds();
    for (const file of files) {
      if (!attachmentMatches(file, kinds)) { toast(`${file.name} is not supported by this model`, true); continue; }
      if (pendingAttachments.length >= 6) { toast('At most 6 attachments are allowed', true); break; }
      if (file.size > 8 * 1024 * 1024) { toast(`${file.name} exceeds the 8 MB limit`, true); continue; }
      const total = pendingAttachments.reduce((sum, item) => sum + item.size, 0) + file.size;
      if (total > 16 * 1024 * 1024) { toast('Attachments exceed the 16 MB total limit', true); break; }
      const dataUrl = await new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result || ''));
        reader.onerror = () => reject(reader.error || new Error('Could not read file'));
        reader.readAsDataURL(file);
      }).catch((err) => { toast(err.message, true); return ''; });
      if (!dataUrl) continue;
      pendingAttachments.push({
        name: file.name, mime: file.type || 'application/octet-stream',
        data: dataUrl.split(',')[1] || '', size: file.size,
        preview: String(file.type || '').startsWith('image/') ? dataUrl : '',
      });
    }
    renderAttachments();
  });

  const assistantRecords = new Map();
  const toolRecords = new Map();
  const reasoningRecords = new Map();
  const completedMessages = new Set();
  const initialCursor = run.last_event_seq || 0;
  let lastSeq = initialCursor;
  let subscribedGen = 0;
  let runEpoch = '';
  let historyReady = false;
  const pendingLiveEvents = [];
  const historyAssistantTexts = new Set();
  let historyOffset = 0;
  let historyTotal = 0;
  let autoScroll = true;
  let currentState = run.state || 'starting';

  const setContext = (open) => {
    chatPage.classList.toggle('context-open', open);
    storeSet('nexus-context-open', open);
  };
  document.getElementById('chat-context-toggle').addEventListener('click', () => setContext(!chatPage.classList.contains('context-open')));
  document.getElementById('chat-inline-context').addEventListener('click', () => setContext(!chatPage.classList.contains('context-open')));
  document.getElementById('context-close').addEventListener('click', () => setContext(false));
  document.getElementById('chat-scrim').addEventListener('click', () => setContext(false));

  const isNearBottom = () => chat.scrollHeight - chat.scrollTop - chat.clientHeight < 90;
  const scrollBottom = (force = false) => {
    if (force || autoScroll) {
      chat.scrollTop = chat.scrollHeight;
      jumpButton.hidden = true;
    } else {
      jumpButton.hidden = false;
    }
  };
  chat.addEventListener('scroll', () => {
    autoScroll = isNearBottom();
    jumpButton.hidden = autoScroll;
  });
  jumpButton.addEventListener('click', () => {
    autoScroll = true;
    scrollBottom(true);
  });

  const setState = (state) => {
    currentState = state || 'unknown';
    run.state = state;
    const label = (state || 'unknown').replaceAll('_', ' ');
    stateNode.textContent = t(label);
    stateNode.className = 'badge ' + (state || 'unknown');
    if (contextState) contextState.textContent = t(label);
    const busy = state === 'running' || state === 'starting';
    stopButton.hidden = !busy;
    workingSlot.innerHTML = busy ? workingHtml() : '';
    const topStop = document.getElementById('stop-run');
    topStop.hidden = !busy;
    topStop.disabled = !busy;
    scrollBottom();
  };
  setState(run.state || 'starting');

  const appendUser = (text) => {
    const article = document.createElement('article');
    article.className = 'turn turn-user';
    article.innerHTML = `<div class="turn-meta"><span>You</span><time>${esc(fmtStamp(new Date()))}</time></div><div class="turn-body"></div>`;
    article.querySelector('.turn-body').textContent = text;
    messagesEl.appendChild(article);
    scrollBottom();
  };

  const assistantRecord = (messageID) => {
    const key = messageID || 'assistant';
    if (assistantRecords.has(key)) return assistantRecords.get(key);
    const article = document.createElement('article');
    article.className = 'turn turn-assistant';
    article.innerHTML = `<div class="turn-meta">${agentGlyph(run.agent || session?.agent, 'small')}<span>${esc(agent.label)}</span><time>${esc(fmtStamp(new Date()))}</time></div><div class="turn-body md"></div>`;
    messagesEl.appendChild(article);
    const rec = { node: article, body: article.querySelector('.turn-body'), raw: '', timer: null, rendered: '' };
    assistantRecords.set(key, rec);
    return rec;
  };

  const renderAssistant = (rec, immediate = false) => {
    const paint = () => {
      if (rec.rendered === rec.raw) return;
      rec.rendered = rec.raw;
      rec.body.innerHTML = renderMarkdown(rec.raw);
      scrollBottom();
    };
    if (immediate) {
      clearTimeout(rec.timer);
      paint();
      return;
    }
    if (rec.timer) return;
    rec.timer = setTimeout(() => {
      rec.timer = null;
      paint();
    }, 70);
  };

  const appendAssistantText = (messageID, text, replace = false) => {
    const rec = assistantRecord(messageID);
    if (replace && text) rec.raw = text;
    else if (text) rec.raw += text;
    renderAssistant(rec, replace);
  };

  const updateTool = (callID, name, input, output, status) => {
    const key = callID || ('tool-' + toolRecords.size);
    let rec = toolRecords.get(key);
    if (!rec) {
      rec = { name: name || 'tool', input: input || '', output: '', status: 'running', node: null };
      toolRecords.set(key, rec);
    }
    if (name) rec.name = name;
    if (input !== undefined) rec.input = input || '';
    if (output !== undefined) rec.output = output || '';
    if (status) rec.status = status;
    const described = describeTool(rec.name, rec.input, rec.output);
    const wrapper = document.createElement('div');
    wrapper.innerHTML = toolCardHtml(described, rec.status);
    const node = wrapper.firstElementChild;
    if (rec.node) {
      node.open = rec.node.open || rec.status === 'error';
      rec.node.replaceWith(node);
    } else {
      messagesEl.appendChild(node);
    }
    rec.node = node;
    scrollBottom();
  };

  const updateReasoning = (messageID, text) => {
    if (!text) return;
    const key = messageID || 'reasoning';
    let rec = reasoningRecords.get(key);
    if (!rec) {
      const wrapper = document.createElement('div');
      wrapper.innerHTML = reasoningHtml(text);
      const node = wrapper.firstElementChild;
      messagesEl.appendChild(node);
      rec = { node, raw: text, timer: null };
      reasoningRecords.set(key, rec);
    } else {
      rec.raw += text;
      if (rec.timer) return;
      rec.timer = setTimeout(() => {
        rec.timer = null;
        rec.node.querySelector('.reasoning-body').innerHTML = renderMarkdown(rec.raw);
        scrollBottom();
      }, 80);
    }
  };

  const showApproval = (event) => {
    const resources = (event.resources || []).join('\n');
    const card = document.createElement('div');
    card.className = 'approval-card';
    card.innerHTML = `
      <div class="approval-head">${svgIcon('alert', 16)}<strong>${t('Approval required')}</strong><span class="badge waiting_for_input">${esc(event.action || 'permission')}</span></div>
      ${resources || event.text ? `<pre>${esc(resources || event.text)}</pre>` : ''}
      <div class="approval-actions"><button class="btn small primary" data-approval="once">${t('Allow once')}</button><button class="btn small danger" data-approval="reject">${t('Deny')}</button></div>`;
    card.querySelectorAll('[data-approval]').forEach((button) => button.addEventListener('click', () => {
      wsSend({ type: 'approval', run_id: id, approval_id: event.approval_id, decision: button.dataset.approval });
      card.remove();
      noteText(t('Approval sent'));
    }));
    approval.appendChild(card);
    autoScroll = true;
    scrollBottom(true);
    noteText('Waiting for your approval');
    setState('waiting_for_input');
  };

  const noteText = (text, live = false) => {
    note.innerHTML = `<span class="connection-dot ${live ? 'live' : ''}"></span>${esc(text)}`;
  };

  const appendError = (text) => {
    const node = document.createElement('div');
    node.className = 'chat-error';
    node.innerHTML = `${svgIcon('alert', 15)}<div><strong>Run error</strong><pre>${esc(text)}</pre></div>`;
    messagesEl.appendChild(node);
    scrollBottom(true);
  };

  const handleEvent = (event) => {
    if (!event || (event.seq && event.seq <= lastSeq)) return;
    if (!historyReady) {
      pendingLiveEvents.push(event);
      return;
    }
    if (event.seq) lastSeq = Math.max(lastSeq, event.seq);
    switch (event.type) {
      case 'message.started':
        assistantRecord(event.message_id);
        break;
      case 'message.delta':
        appendAssistantText(event.message_id, event.text || '');
        break;
      case 'message.completed': {
        const key = event.message_id || 'assistant';
        if (completedMessages.has(key)) break;
        completedMessages.add(key);
        appendAssistantText(event.message_id, event.text || '', true);
        renderAssistant(assistantRecord(event.message_id), true);
        break;
      }
      case 'reasoning':
        updateReasoning(event.message_id, event.text || '');
        break;
      case 'tool.started':
        updateTool(event.call_id, event.tool_name, event.tool_input, undefined, 'running');
        break;
      case 'tool.input':
        updateTool(event.call_id, undefined, event.tool_input, undefined, 'running');
        break;
      case 'tool.completed':
        updateTool(event.call_id, undefined, undefined, event.error ? `error: ${event.error}` : (event.tool_output || ''), event.error ? 'error' : 'done');
        break;
      case 'approval.requested':
        showApproval(event);
        break;
      case 'approval.resolved':
        approval.innerHTML = '';
        noteText(`Approval ${event.decision || 'resolved'}`, true);
        break;
      case 'run.state':
        setState(event.state);
        noteText(event.state === 'idle' ? 'Idle — ready for your next message' : `Agent ${event.state.replaceAll('_', ' ')}`, event.state === 'running');
        break;
      case 'run.error':
        setState('failed');
        noteText(event.error || 'run failed');
        appendError(event.error || 'run failed');
        break;
    }
  };

  const flushPendingLiveEvents = () => {
    pendingLiveEvents.sort((a, b) => (a.seq || 0) - (b.seq || 0));
    for (const event of pendingLiveEvents.splice(0)) {
      if (event.type === 'message.completed') {
        const text = String(event.text || '').trim();
        if (text && historyAssistantTexts.has(text)) {
          historyAssistantTexts.delete(text);
          const key = event.message_id || 'assistant';
          const rec = assistantRecords.get(key);
          if (rec?.node) {
            rec.node.remove();
            assistantRecords.delete(key);
          }
          if (event.seq) lastSeq = Math.max(lastSeq, event.seq);
          continue;
        }
      }
      handleEvent(event);
    }
  };

  const handler = (msg) => {
    if (msg.run_id !== id && msg.type !== 'ready') return;
    if (msg.type === 'run.event' && msg.event) handleEvent(msg.event);
    if (msg.type === 'run.state') {
      const epochChanged = Boolean(msg.epoch && runEpoch && msg.epoch !== runEpoch);
      if (msg.epoch) runEpoch = msg.epoch;
      if (epochChanged) {
        // The hub restarted and its per-run sequence starts from zero again.
        lastSeq = 0;
        sendSubscribe();
      }
      setState(msg.state);
      noteText(msg.state === 'idle' ? 'Idle — ready for your next message' : `Agent ${msg.state.replaceAll('_', ' ')}`, msg.state === 'running');
    }
    if (msg.type === 'run.exit') {
      setState(msg.state || 'exited');
      noteText(msg.message || 'Run exited');
    }
    if (msg.type === 'error') toast(msg.error || 'error', true);
  };
  state.wsHandlers.add(handler);

  const sendSubscribe = () => {
    const last = Math.max(lastSeq, initialCursor);
    if (wsSend({ type: 'subscribe', run_id: id, last_seq: last })) subscribedGen = state.wsGen;
  };

  const loadEarlier = async () => {
    if (!historyOffset) return;
    const before = chat.scrollHeight;
    const top = chat.scrollTop;
    const start = Math.max(0, historyOffset - 120);
    const tr = await api(`/sessions/${encodeURIComponent(run.session_id)}/transcript?offset=${start}&limit=${historyOffset - start}`);
    if (tr.messages?.length) {
      messagesEl.insertAdjacentHTML('afterbegin', renderTranscriptMessages(tr.messages, run.agent || session?.agent));
      chat.scrollTop = top + (chat.scrollHeight - before);
    }
    historyOffset = start;
    historyTop.innerHTML = historyOffset > 0
      ? `<button class="btn small ghost history-button" id="load-earlier">${t('Load earlier messages ({n} remaining)', { n: historyOffset.toLocaleString() })}</button>`
      : '';
    document.getElementById('load-earlier')?.addEventListener('click', () => loadEarlier().catch((err) => toast(err.message, true)));
  };

  const loadHistory = async () => {
    if (!run.session_id) return;
    try {
      const head = await api(`/sessions/${encodeURIComponent(run.session_id)}/transcript?offset=0&limit=1`);
      historyTotal = head.total || 0;
      if (!historyTotal) return;
      const limit = Math.min(200, historyTotal);
      const offset = Math.max(0, historyTotal - limit);
      const tr = historyTotal > 1 ? await api(`/sessions/${encodeURIComponent(run.session_id)}/transcript?offset=${offset}&limit=${limit}`) : head;
      historyOffset = offset;
      const visible = (tr.messages || []).filter((m) => m.kind === 'tool_call' || m.kind === 'tool_result' || m.role !== 'user' || !/^\s*<(environment_context|recommended_plugins|user_instructions|permissions|skills)/i.test(m.text || ''));
      for (const message of visible) {
        if (message.role === 'assistant' && (!message.kind || message.kind === 'text')) {
          const text = String(message.text || '').trim();
          if (text) historyAssistantTexts.add(text);
        }
      }
      messagesEl.innerHTML = renderTranscriptMessages(visible, run.agent || session?.agent);
      historyTop.innerHTML = offset > 0
        ? `<button class="btn small ghost history-button" id="load-earlier">${t('Load earlier messages ({n} remaining)', { n: offset.toLocaleString() })}</button>`
        : '';
      document.getElementById('load-earlier')?.addEventListener('click', () => loadEarlier().catch((err) => toast(err.message, true)));
      autoScroll = true;
      scrollBottom(true);
    } catch { /* history is optional */ }
    historyReady = true;
    flushPendingLiveEvents();
  };

  api(`/runs/${id}/lease`, { method: 'POST' }).then(() => {
    noteText(t('You have control of this run'), true);
  }).catch((err) => {
    noteText(t('Read-only:') + ' ' + err.message);
    textarea.disabled = true;
    sendButton.disabled = true;
  });

  document.getElementById('stop-run').addEventListener('click', async () => {
    try { await api(`/runs/${id}/stop`, { method: 'POST' }); toast(t('Stopping run')); }
    catch (err) { toast(err.message, true); }
  });
  stopButton.addEventListener('click', () => {
    wsSend({ type: 'interrupt', run_id: id });
    noteText(t('Interrupt requested'));
  });

  const draft = storeGet(draftKey(id), '');
  if (draft) textarea.value = draft;
  const autosize = () => {
    textarea.style.height = 'auto';
    textarea.style.height = `${Math.min(220, textarea.scrollHeight)}px`;
  };
  autosize();
  textarea.addEventListener('input', () => {
    autosize();
    storeSet(draftKey(id), textarea.value);
  });
  const coarsePointer = window.matchMedia('(pointer: coarse)').matches;
  textarea.addEventListener('keydown', (event) => {
    if (event.isComposing) return;
    if (event.key === 'Enter' && !event.shiftKey && (!coarsePointer || event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      form.requestSubmit();
    }
  });
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    const text = textarea.value.trim();
    if (!text) return;
    const promptOptions = currentPromptOptions();
    const message = { type: 'prompt', run_id: id, text };
    if (Object.keys(promptOptions).length) message.options = promptOptions;
    if (!wsSend(message)) {
      toast(t('WebSocket is reconnecting; try again in a moment'), true);
      return;
    }
    appendUser(text);
    textarea.value = '';
    pendingAttachments = [];
    renderAttachments();
    storeSet(draftKey(id), '');
    autosize();
    autoScroll = true;
    setState('running');
    noteText(t('Agent is working'), true);
    scrollBottom(true);
  });

  if (session) {
    document.getElementById('context-pin').addEventListener('click', async () => {
      const on = !state.favorites.has(session.id);
      try {
        await api(`/sessions/${session.id}/favorite`, { method: on ? 'PUT' : 'DELETE' });
        if (on) state.favorites.add(session.id); else state.favorites.delete(session.id);
        document.getElementById('context-pin').innerHTML = `${svgIcon('star')} ${on ? 'Unpin session' : 'Pin session'}`;
        loadSidebar().catch(() => {});
      } catch (err) { toast(err.message, true); }
    });
    document.getElementById('context-terminal').addEventListener('click', () => startRun(session, false, 'terminal'));
  }
  const loadGit = async () => {
    if (!session) return;
    const box = document.getElementById('context-git');
    box.innerHTML = '<div class="empty compact"><span class="spinner"></span></div>';
    try {
      const g = await api(`/sessions/${session.id}/git`);
      box.innerHTML = `
        <div class="git-context-head">${g.branch ? `<span class="branch-chip">${svgIcon('branch', 12)}${esc(g.branch)}</span>` : ''}<button class="btn ghost small" id="git-refresh" title="Refresh changes">${svgIcon('refresh', 13)}</button></div>
        ${g.root ? `<div class="copy-line"><code>${esc(g.root)}</code></div>` : ''}
        <pre class="git-status">${esc(g.status?.trim() || 'Clean working tree')}</pre>
        ${g.stat ? `<pre class="git-stat-small">${esc(g.stat)}</pre>` : ''}
        ${g.diff ? `<details class="git-diff" open><summary>Uncommitted changes</summary>${renderDiffBlock(g.diff)}</details>` : ''}`;
    } catch (err) {
      box.innerHTML = `<div class="notice error small">${esc(err.message)}</div><button class="btn small block mt-1" id="context-git-load">Try again</button>`;
    }
  };
  document.getElementById('context-git').addEventListener('click', (event) => {
    if (event.target.closest('#context-git-load') || event.target.closest('#git-refresh')) loadGit();
  });

  sendSubscribe();
  await loadHistory().catch(() => {});
  loadRunOptions().catch(() => {});
  const timer = setInterval(() => {
    if (!state.me) { clearInterval(timer); return; }
    if (state.ws?.readyState === WebSocket.OPEN) {
      if (subscribedGen !== state.wsGen) sendSubscribe();
      if (note.textContent.includes('Connecting')) noteText('Connected. Send a message.');
    } else {
      noteText('Reconnecting…');
    }
  }, 800);

  state.pageCleanup = () => {
    clearInterval(timer);
    state.wsHandlers.delete(handler);
  };
  textarea.focus();
}
async function pageRuns() {
  renderShell('runs');
  const view = setPage(t('Control room'), t('All active terminal and structured runs across your machines.'), `<button class="btn primary small" id="runs-new">${t('New session')}</button><a class="btn small" href="#/workspace">${t('Split view')}</a><a class="btn small" href="#/sessions">${t('All sessions')}</a>`);
  view.innerHTML = `
    <div class="control-summary" id="run-summary"></div>
    <div class="filterbar compact">
      <div class="segmented" id="run-filter" role="group" aria-label="${t('Filter runs')}">
        <button class="segment active" data-run-filter="all">${t('All')}</button>
        <button class="segment" data-run-filter="running">${t('Running')}</button>
        <button class="segment" data-run-filter="waiting_for_input">${t('Waiting')}</button>
      </div>
      <span class="filter-count" id="run-count"></span>
    </div>
    <div id="runs-list"><div class="empty"><span class="spinner"></span></div></div>`;
  document.getElementById('runs-new').addEventListener('click', newSessionPicker);
  let runs = [];
  let filter = 'all';
  let timer = null;
  const render = () => {
    const box = document.getElementById('runs-list');
    if (!box) return;
    const running = runs.filter((r) => r.state === 'running' || r.state === 'starting').length;
    const waiting = runs.filter((r) => r.state === 'waiting_for_input').length;
    document.getElementById('run-summary').innerHTML = `
      <div><strong>${runs.length}</strong><span>${t('open runs')}</span></div>
      <div><strong>${running}</strong><span>${t('working')}</span></div>
      <div><strong class="${waiting ? 'warn' : ''}">${waiting}</strong><span>${t('waiting for you')}</span></div>
      <div><strong>${new Set(runs.map((r) => r.host_id)).size}</strong><span>${t('hosts')}</span></div>`;
    document.getElementById('run-count').textContent = countLabel(runs.length, 'open run', 'open runs');
    const visible = runs.filter((r) => filter === 'all' || (filter === 'running' ? (r.state === 'running' || r.state === 'starting') : r.state === filter));
    if (!visible.length) {
      box.innerHTML = `<div class="card empty"><strong>${runs.length ? t('No runs in this state') : t('No active runs')}</strong>${runs.length ? t('Choose another filter.') : t('Resume a native session or start a new agent on an online host.')}</div>`;
      return;
    }
    const shared = new Map();
    for (const r of runs) {
      if (!r.cwd) continue;
      const key = `${r.host_id}|${cwdKey(r.cwd)}`;
      shared.set(key, (shared.get(key) || 0) + 1);
    }
    box.innerHTML = `<div class="run-control-grid">${visible.map((run) => {
      const key = `${run.host_id}|${cwdKey(run.cwd)}`;
      return `<article class="run-control">
        <div class="run-control-head">${agentGlyph(run.agent)}${stateBadge(run.state)}<span class="run-elapsed">${fmtDuration(run.started_at)}</span></div>
        <a class="run-control-title" href="#/runs/${esc(run.id)}">${esc(run.session_title || t('Untitled session'))}</a>
        <div class="run-control-meta">${svgIcon('folder', 13)}${esc(projectName(run.cwd))}<span>·</span>${esc(run.host_name || run.host_id)}<span>·</span>${esc(run.mode)}</div>
        ${(shared.get(key) || 0) > 1 ? `<div class="shared-warning">${svgIcon('alert', 13)} ${t('{n} agents share this worktree', { n: shared.get(key) })}</div>` : ''}
        <div class="run-control-actions">
          <a class="btn small" href="#/runs/${esc(run.id)}">${t('Open')}</a>
          ${run.session_id ? `<a class="btn small ghost" href="#/sessions/${esc(run.session_id)}">${t('Transcript')}</a>` : ''}
          <button class="btn small danger right" data-stop-run="${esc(run.id)}">${t('Stop')}</button>
        </div>
      </article>`;
    }).join('')}</div>`;
    box.querySelectorAll('[data-stop-run]').forEach((button) => button.addEventListener('click', async () => {
      try { await api(`/runs/${button.dataset.stopRun}/stop`, { method: 'POST' }); toast(t('Stopping run')); }
      catch (err) { toast(err.message, true); }
    }));
  };
  const load = async () => {
    const data = await api('/runs');
    runs = data.runs || [];
    render();
  };
  const schedule = () => {
    clearTimeout(timer);
    timer = setTimeout(() => load().catch(() => {}), 400);
  };
  document.getElementById('run-filter').addEventListener('click', (event) => {
    const button = event.target.closest('[data-run-filter]');
    if (!button) return;
    filter = button.dataset.runFilter;
    document.querySelectorAll('#run-filter .segment').forEach((node) => node.classList.toggle('active', node === button));
    render();
  });
  state.wsHandlers.add((msg) => {
    if (msg.type === 'run.state' || msg.type === 'run.exit' || msg.type === 'run.error' ||
        msg.type === 'sessions.changed' || msg.type === 'host.updated') schedule();
  });
  await load();
}

async function pageWorkspace() {
  renderShell('runs');
  const view = setPage(t('Workspace'), t('Side-by-side agent sessions from any host.'), `<button class="btn small" id="workspace-add">${t('Add session')}</button><a class="btn small" href="#/runs">${t('Control room')}</a>`);
  const { runs } = await api('/runs');
  if (!runs.length) {
    view.innerHTML = `<div class="card empty"><strong>${t('No open runs')}</strong>${t('Resume a native session or start a new agent before opening a split workspace.')}</div>`;
    return;
  }
  const byID = new Map(runs.map((run) => [run.id, run]));
  const params = new URLSearchParams(location.hash.split('?')[1] || '');
  const stored = storeGet('nexus-workspace', { panes: [], orientation: 'horizontal', ratio: 50 });
  let panes = [...new Set(params.getAll('pane'))].filter((id) => byID.has(id));
  if (!panes.length) panes = (stored.panes || []).filter((id) => byID.has(id));
  for (const run of runs) {
    if (panes.length >= 2) break;
    if (!panes.includes(run.id)) panes.push(run.id);
  }
  panes = panes.slice(0, 2);
  let orientation = params.get('orientation') === 'vertical' ? 'vertical' : (stored.orientation === 'vertical' ? 'vertical' : 'horizontal');
  let ratio = Math.min(75, Math.max(25, Number(params.get('ratio')) || Number(stored.ratio) || 50));
  let dragging = false;

  const persist = () => {
    storeSet('nexus-workspace', { panes, orientation, ratio });
    const query = new URLSearchParams();
    panes.forEach((id) => query.append('pane', id));
    query.set('orientation', orientation);
    query.set('ratio', String(Math.round(ratio)));
    history.replaceState(null, '', `#/workspace?${query.toString()}`);
  };
  const choosePane = () => {
    const available = runs.filter((run) => !panes.includes(run.id));
    if (!available.length) { toast(t('All open runs are already visible'), true); return; }
    modal(`
      <h2>${t('Add a session to the workspace')}</h2>
      <p class="hint">${t('Each pane keeps its own live connection and approval controls. A native session can only be started once; this picker only opens existing runs.')}</p>
      <div class="list workspace-picker">${available.map((run) => `
        <button class="list-row" type="button" data-pick-run="${esc(run.id)}">
          <span class="workspace-pick-main">${agentGlyph(run.agent)}<span><strong>${esc(run.session_title || t('Untitled session'))}</strong><small>${esc(projectName(run.cwd))} · ${esc(run.host_name || run.host_id)} · ${esc(run.agent)}</small></span></span>
          ${stateBadge(run.state)}
        </button>`).join('')}</div>
      <div class="modal-actions"><button class="btn ghost" type="button" data-close>Cancel</button></div>`);
    modalRoot.querySelectorAll('[data-pick-run]').forEach((button) => button.addEventListener('click', () => {
      const id = button.dataset.pickRun;
      if (!panes.includes(id)) panes.push(id);
      panes = panes.slice(0, 2);
      closeModal();
      persist();
      render();
    }));
  };

  const render = () => {
    const paneMarkup = panes.map((id, index) => {
      const run = byID.get(id);
      if (!run) return '';
      return `<section class="split-pane">
        <div class="pane-head">
          ${agentGlyph(run.agent)}
          <div class="pane-title"><strong>${esc(run.session_title || t('Untitled session'))}</strong><small>${esc(projectName(run.cwd))} · ${esc(run.host_name || run.host_id)} · ${esc(run.agent)}</small></div>
          <span class="badge ${esc(run.state)} pane-state" data-pane-state="${esc(id)}">${esc((run.state || 'unknown').replaceAll('_', ' '))}</span>
          <a class="icon-btn" href="#/runs/${esc(id)}" title="${t('Open in single view')}" aria-label="${t('Open in single view')}">${svgIcon('external', 14)}</a>
          ${panes.length > 1 ? `<button class="icon-btn" type="button" data-swap-pane="${esc(id)}" title="${t('Swap panes')}" aria-label="${t('Swap panes')}">${svgIcon('arrowDown', 14)}</button><button class="icon-btn" type="button" data-close-pane="${esc(id)}" title="${t('Close pane')}" aria-label="${t('Close pane')}">${svgIcon('x', 14)}</button>` : ''}
        </div>
        <div class="pane-frame">
          <div class="pane-loading"><span class="spinner"></span></div>
          <iframe title="${esc(run.session_title || 'Agent session')}" loading="eager" src="/?embed=1#/runs/${encodeURIComponent(id)}"></iframe>
        </div>
      </section>`;
    }).join(panes.length === 2 ? `<div class="split-divider" id="split-divider" role="separator" tabindex="0" title="${t('Drag or use arrow keys to resize panes')}" aria-label="${t('Resize panes')}"><span></span></div>` : '');
    view.innerHTML = `
      <div class="workspace-page">
        <div class="workspace-head">
          <div><strong>${countLabel(panes.length, 'visible session', 'visible sessions')}</strong><span>${t('Panels keep independent streams and approval prompts.')}</span></div>
          <div class="workspace-actions">
            <button class="btn small" id="workspace-add-inline" type="button">${svgIcon('plus')} ${t('Add pane')}</button>
            <button class="btn small" id="workspace-orientation" type="button">${orientation === 'horizontal' ? t('Side by side') : t('Stacked')}</button>
          </div>
        </div>
        <div class="split-grid ${orientation} ${panes.length === 2 ? 'two' : 'single'}" id="split-grid" style="--split:${ratio}%">
          ${paneMarkup}
        </div>
      </div>`;
    document.getElementById('workspace-add').onclick = choosePane;
    document.getElementById('workspace-add-inline').onclick = choosePane;
    document.getElementById('workspace-orientation')?.addEventListener('click', () => {
      orientation = orientation === 'horizontal' ? 'vertical' : 'horizontal';
      persist();
      render();
    });
    view.querySelectorAll('[data-close-pane]').forEach((button) => button.addEventListener('click', () => {
      panes = panes.filter((id) => id !== button.dataset.closePane);
      persist();
      render();
    }));
    view.querySelectorAll('[data-swap-pane]').forEach((button) => button.addEventListener('click', () => {
      if (panes.length === 2) panes = [panes[1], panes[0]];
      persist();
      render();
    }));
    view.querySelectorAll('iframe').forEach((frame) => frame.addEventListener('load', () => frame.parentElement?.querySelector('.pane-loading')?.remove()));
    const divider = document.getElementById('split-divider');
    if (divider) {
      divider.addEventListener('pointerdown', (event) => {
        dragging = true;
        divider.setPointerCapture(event.pointerId);
        document.body.style.cursor = orientation === 'horizontal' ? 'col-resize' : 'row-resize';
        document.body.style.userSelect = 'none';
      });
      divider.addEventListener('pointermove', (event) => {
        if (!dragging) return;
        const grid = document.getElementById('split-grid');
        const rect = grid.getBoundingClientRect();
        const raw = orientation === 'horizontal'
          ? ((event.clientX - rect.left) / rect.width) * 100
          : ((event.clientY - rect.top) / rect.height) * 100;
        ratio = Math.min(75, Math.max(25, raw));
        grid.style.setProperty('--split', `${ratio}%`);
      });
      const end = () => {
        if (!dragging) return;
        dragging = false;
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        persist();
      };
      divider.addEventListener('pointerup', end);
      divider.addEventListener('pointercancel', end);
      divider.setAttribute('aria-orientation', orientation === 'horizontal' ? 'vertical' : 'horizontal');
      divider.addEventListener('keydown', (event) => {
        const step = event.shiftKey ? 10 : 3;
        const decrease = orientation === 'horizontal' ? 'ArrowLeft' : 'ArrowUp';
        const increase = orientation === 'horizontal' ? 'ArrowRight' : 'ArrowDown';
        if (event.key !== decrease && event.key !== increase) return;
        event.preventDefault();
        ratio = Math.min(75, Math.max(25, ratio + (event.key === increase ? step : -step)));
        document.getElementById('split-grid')?.style.setProperty('--split', `${ratio}%`);
        persist();
      });
    }
  };

  state.wsHandlers.add((msg) => {
    if (msg.type !== 'run.state' && msg.type !== 'run.exit' && msg.type !== 'run.error') return;
    const node = view.querySelector(`[data-pane-state="${msg.runID || msg.run_id}"]`);
    if (!node) return;
    const next = msg.type === 'run.state' ? msg.state : (msg.type === 'run.exit' ? (msg.state || 'exited') : 'failed');
    if (!next) return;
    node.textContent = String(next).replaceAll('_', ' ');
    node.className = `badge ${next} pane-state`;
  });
  persist();
  render();
}

async function pageRun(id) {
  renderShell('sessions');
  const run = await api(`/runs/${id}`);
  state.run = run;
  if (run.mode === 'structured') return pageStructuredRun(id, run);
  const title = run.session_title || t('Terminal');
  openTab({ id, key: run.session_id || id, kind: 'run', href: `#/runs/${id}`, title, agent: run.agent });
  rememberRecent({ id, title, agent: run.agent, hint: `${agentMeta(run.agent).label} · ${run.host_name || run.host_id} · terminal`, href: `#/runs/${id}` });
  setPage(title, `Terminal · ${projectName(run.cwd)} · ${run.host_name || run.host_id}`, `<button class="btn small danger" id="stop-run">${svgIcon('stop')} ${t('Stop')}</button><a class="btn small ghost" href="#/sessions">${t('Sessions')}</a>`);
  const view = document.getElementById('view');
  view.innerHTML = `
    <div class="terminal-page">
      <div class="terminal-statusbar">
        ${stateBadge(run.state)}
        ${agentGlyph(run.agent)}
        <span>${esc(agentMeta(run.agent).label)}</span><span class="chat-sep">·</span><span>${esc(projectName(run.cwd))}</span><span class="chat-sep">·</span><span>${esc(run.host_name || run.host_id)}</span>
        <span class="chat-connection right" id="run-note"><span class="connection-dot"></span>Connecting…</span>
      </div>
      <div class="terminal-shell">
        <div class="terminal-bar"><span class="dot" id="term-dot"></span><span id="term-status">${t('disconnected')}</span><span class="right mono-small">${t('lease')} ${esc(shortId(run.lease_device_id || 'none'))} · ${esc(run.id)}</span></div>
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
      </div>
    </div>`;
  const term = new Terminal({
    cursorBlink: true,
    fontFamily: 'JetBrains Mono, Cascadia Code, SFMono-Regular, Consolas, monospace',
    fontSize: 13,
    lineHeight: 1.25,
    theme: {
      background: '#0f1114', foreground: '#e7e8ea', cursor: '#7d8cf8', selectionBackground: '#2a3352',
      black: '#15171b', red: '#e06c75', green: '#7bbf8a', yellow: '#d6b26f', blue: '#7d9df8',
      magenta: '#b49af8', cyan: '#7cc7c7', white: '#e7e8ea',
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
  const setLive = (text) => { status.textContent = t(text); dot.classList.toggle('live', text === 'connected'); };
  let lastSeq = 0;
  let subscribedGen = 0;
  let runEpoch = '';
  const input = (data) => {
    if (run.lease_device_id && state.me.device.id && run.lease_device_id !== state.me.device.id) {
      note.textContent = t('Read-only: another device holds the lease.');
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
    note.textContent = t('You have control (controller lease).');
  }).catch((err) => { note.textContent = t('Read-only:') + ' ' + err.message; });

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
        if (msg.epoch && runEpoch && msg.epoch !== runEpoch) {
          lastSeq = 0;
          sendSubscribe();
        }
        if (msg.epoch) runEpoch = msg.epoch;
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

/* ---------- device pairing ---------- */

function renderDevicePair(token) {
  authFrame(`
    <h2 class="mt-0">Pair this device</h2>
    <p class="hint mb-2">A signed-in device requested this pairing. Give this browser a name and confirm; the link is single-use.</p>
    <form class="form" id="pair-form">
      <label class="label">Device name<input class="input" name="name" placeholder="Phone, tablet, second laptop..." required maxlength="80"></label>
      <button class="btn primary block" type="submit">Pair this device</button>
    </form>`, 'Device pairing');
  document.getElementById('pair-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const name = new FormData(event.target).get('name');
    try {
      await api('/auth/device-pair', { method: 'POST', body: { token, name }, allow401: true });
      state.me = await api('/me');
      state.hubName = state.me.hub_name || state.hubName;
      await loadUserTheme();
      history.replaceState({}, '', '/');
      renderShell('overview');
      await route();
    } catch (err) {
      renderDevicePair(token);
      toast(err.message, true);
    }
  });
}

/* ---------- boot ---------- */

function urlBase64ToUint8Array(base64String) {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(base64);
  const output = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) output[i] = raw.charCodeAt(i);
  return output;
}

function b64urlToBuf(value) {
  const normalized = String(value).replace(/-/g, '+').replace(/_/g, '/');
  const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4);
  const raw = atob(padded);
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}

function bufToB64url(buffer) {
  const bytes = new Uint8Array(buffer);
  let s = '';
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function prepareCreation(publicKey) {
  const pk = { ...publicKey };
  pk.challenge = b64urlToBuf(pk.challenge);
  if (pk.user) pk.user = { ...pk.user, id: b64urlToBuf(pk.user.id) };
  if (Array.isArray(pk.excludeCredentials)) {
    pk.excludeCredentials = pk.excludeCredentials.map((c) => ({ ...c, id: b64urlToBuf(c.id) }));
  }
  return pk;
}

function prepareAssertion(publicKey) {
  const pk = { ...publicKey };
  pk.challenge = b64urlToBuf(pk.challenge);
  if (Array.isArray(pk.allowCredentials)) {
    pk.allowCredentials = pk.allowCredentials.map((c) => ({ ...c, id: b64urlToBuf(c.id) }));
  }
  return pk;
}

function serializeCredential(cred, kind) {
  const response = kind === 'create'
    ? {
        clientDataJSON: bufToB64url(cred.response.clientDataJSON),
        attestationObject: bufToB64url(cred.response.attestationObject),
        transports: cred.response.getTransports ? cred.response.getTransports() : undefined,
      }
    : {
        clientDataJSON: bufToB64url(cred.response.clientDataJSON),
        authenticatorData: bufToB64url(cred.response.authenticatorData),
        signature: bufToB64url(cred.response.signature),
        userHandle: cred.response.userHandle ? bufToB64url(cred.response.userHandle) : null,
      };
  return {
    id: cred.id,
    rawId: bufToB64url(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment || undefined,
    response,
    clientExtensionResults: cred.getClientExtensionResults(),
  };
}

async function registerPasskey() {
  if (!window.PublicKeyCredential) throw new Error('WebAuthn is not supported by this browser');
  const begin = await api('/auth/passkey/register/begin', { method: 'POST' });
  const cred = await navigator.credentials.create({ publicKey: prepareCreation(begin.publicKey) });
  if (!cred) throw new Error('Passkey creation was cancelled');
  await api(`/auth/passkey/register/finish?session=${encodeURIComponent(begin.session_id)}&name=${encodeURIComponent('This device')}`, {
    method: 'POST',
    body: serializeCredential(cred, 'create'),
  });
}

async function passkeyLogin(username) {
  if (!window.PublicKeyCredential) throw new Error('WebAuthn is not supported by this browser');
  const begin = await api('/auth/passkey/begin', { method: 'POST', body: { username }, allow401: true });
  const cred = await navigator.credentials.get({ publicKey: prepareAssertion(begin.publicKey) });
  if (!cred) throw new Error('Passkey sign-in was cancelled');
  await api(`/auth/passkey/finish?session=${encodeURIComponent(begin.session_id)}`, {
    method: 'POST',
    body: serializeCredential(cred, 'get'),
    allow401: true,
  });
}

let stepUpFlight = null;
function ensureStepUp() {
  if (!stepUpFlight) {
    stepUpFlight = performStepUp().finally(() => { stepUpFlight = null; });
  }
  return stepUpFlight;
}

async function performStepUp() {
  const status = await api('/auth/step-up');
  if (status.valid_until && new Date(status.valid_until) > new Date()) return;
  const begin = await api('/auth/step-up/begin', { method: 'POST' });
  if (begin.method === 'passkey') {
    if (!window.PublicKeyCredential) throw new Error('WebAuthn is not supported by this browser');
    const cred = await navigator.credentials.get({ publicKey: prepareAssertion(begin.publicKey) });
    if (!cred) throw new Error('Step-up confirmation was cancelled');
    await api(`/auth/step-up/passkey?session=${encodeURIComponent(begin.session_id)}`, {
      method: 'POST',
      body: serializeCredential(cred, 'get'),
      allow401: true,
    });
  } else {
    await stepUpPasswordDialog(begin.totp_enabled);
  }
  toast('Confirmed');
}

function stepUpPasswordDialog(totpEnabled) {
  return new Promise((resolve, reject) => {
    modal(`
      <h2>Confirm it's you</h2>
      <p class="hint">This change needs a fresh confirmation. Enter your password${totpEnabled ? ' and authenticator code' : ''}.</p>
      <form class="form" id="step-up-form">
        <label class="label">Password<input class="input" type="password" name="password" required autocomplete="current-password"></label>
        ${totpEnabled ? '<label class="label">One-time code<input class="input" name="totp" inputmode="numeric" autocomplete="one-time-code" required></label>' : ''}
        <div class="modal-actions">
          <button class="btn ghost" type="button" data-close>Cancel</button>
          <button class="btn primary" type="submit">Confirm</button>
        </div>
      </form>`, () => reject(new Error('step-up cancelled')));
    document.getElementById('step-up-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const form = new FormData(event.target);
      try {
        await api('/auth/step-up/password', {
          method: 'POST',
          body: { password: form.get('password'), totp: form.get('totp') || '' },
          allow401: true,
        });
        resolve();
        closeModal();
      } catch (err) {
        toast(err.message, true);
      }
    });
  });
}

async function enablePush() {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    throw new Error('Push is not supported by this browser');
  }
  const registration = await navigator.serviceWorker.register('/sw.js');
  const permission = await Notification.requestPermission();
  if (permission !== 'granted') throw new Error('Notification permission was not granted');
  const key = await api('/push/key');
  const subscription = await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(key.public_key),
  });
  const json = subscription.toJSON();
  await api('/push/subscribe', { method: 'POST', body: {
    endpoint: json.endpoint,
    keys: { p256dh: json.keys.p256dh, auth: json.keys.auth },
  } });
}

async function boot() {
  state.embed = new URLSearchParams(location.search).get('embed') === '1';
  if (state.embed) document.documentElement.dataset.embed = '1';
  installGlobalUi();
  if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {});
  const pairToken = new URLSearchParams(location.search).get('pair');
  if (pairToken) return renderDevicePair(pairToken);
  try {
    const status = await api('/setup/status', { allow401: true });
    state.setupRequired = status.setup_required;
    state.hubName = status.hub_name || state.hubName;
    state.version = status.version || state.version;
    if (status.setup_required) return renderAuth('setup');
    try {
      state.me = await api('/me', { allow401: true });
      await loadUserTheme();
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
