import { api } from './api.js';
import { logout } from './auth.js';
import { loadNotifications, toggleNotifications } from './notifications.js';
import { closePalette, openPalette } from './palette.js';
import { state, storeGet, storeSet } from './state.js';
import { closeTab, loadTabs, renderTabs } from './tabs.js';
import { agentGlyph, app, badge, closeModal, copyText, modal, modalRoot, toast } from './ui.js';
import { connectWS } from './ws.js';
import { applyDiffMode } from './components/transcript.js';
import { t } from './i18n.js';
import { svgIcon } from './icons.js';
import { countLabel, cwdKey, esc } from './ui-format.js';

export const navItems = [
  ['#/', 'Home', 'home', 'overview'],
  ['#/projects', 'Projects', 'projects', 'projects'],
  ['#/sessions', 'Sessions', 'sessions', 'sessions'],
  ['#/tasks', 'Tasks', 'task', 'tasks'],
  ['#/usage', 'Usage', 'usage', 'usage'],
  ['#/fanout', 'Fan-out', 'branch', 'fanout'],
  ['#/runs', 'Active', 'runs', 'runs'],
  ['#/hosts', 'Hosts', 'hosts', 'hosts'],
  ['#/settings', 'Settings', 'settings', 'settings'],
];

export function renderShell(active = 'overview') {
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
            <span class="brand-text"><strong>AgentNexus</strong>${state.hubName && state.hubName !== 'AgentNexus' ? `<small>${esc(state.hubName)}</small>` : ''}</span>
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

export function themeIcon() {
  const mode = state.settings.theme || 'system';
  return svgIcon(mode === 'dark' ? 'moon' : mode === 'light' ? 'sun' : 'monitor');
}

export function toggleSidebar() {
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

export function sidebarSession(s) {
  const title = s.title || t('Untitled session');
  return `<a class="side-row side-session" href="#/sessions/${esc(s.id)}" title="${esc(title)}">
    <span class="side-state ${s.state === 'running' ? 'running' : s.state === 'waiting_for_input' ? 'waiting' : ''}"></span>
    ${agentGlyph(s.agent, 'small')}
    <span class="truncate">${esc(title)}</span>
  </a>`;
}

export async function loadSidebar() {
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

export async function saveTheme(theme) {
  const saved = await api('/me/settings', { method: 'PUT', body: { theme } });
  state.settings = { ...state.settings, theme: saved.theme };
  window.NexusTheme?.apply(saved.theme);
  const button = document.getElementById('theme-toggle');
  if (button) {
    button.innerHTML = themeIcon();
    button.title = `Theme: ${saved.theme}`;
  }
}

export async function loadUserTheme() {
  try {
    state.settings = await api('/me/settings');
    window.NexusTheme?.apply(state.settings.theme || 'system');
  } catch { /* keep the local/system theme */ }
}

export function setPage(title, sub = '', actions = '') {
  document.getElementById('page-title').textContent = title;
  document.getElementById('page-sub').textContent = sub;
  document.getElementById('page-actions').innerHTML = actions;
  return document.getElementById('view');
}

let goPending = false;
let goTimer = null;

function openShortcutHelp() {
  const rows = [
    [t('Command palette'), 'Ctrl / ⌘ + K'],
    [t('This help'), '?'],
    [t('Go home'), 'g h'],
    [t('Go to projects'), 'g p'],
    [t('Go to sessions'), 'g s'],
    [t('Go to active runs'), 'g a'],
    [t('Go to usage'), 'g u'],
    [t('Go to fan-out'), 'g f'],
    [t('Go to hosts'), 'g o'],
    [t('Go to settings'), 'g ,'],
    [t('Switch tab'), 'Alt + 1…9'],
    [t('Close active tab'), 'Alt + W'],
    [t('Toggle details panel'), 'Ctrl / ⌘ + I'],
    [t('Send prompt'), 'Enter'],
    [t('New line in prompt'), 'Shift + Enter'],
    [t('Send without newline'), 'Ctrl / ⌘ + Enter'],
    [t('Allow approval'), 'a'],
    [t('Deny approval'), 'd'],
    [t('Interrupt current turn'), 'x'],
    [t('Close dialog or panel'), 'Escape'],
    [t('Resize split workspace'), t('Arrow keys on the divider')],
  ];
  modal(`<div class="split"><h2>${t('Keyboard shortcuts')}</h2><span class="muted small">${t('Press ? anywhere outside an input')}</span></div>
    <div class="shortcut-list">${rows.map(([label, key]) => `<div class="shortcut-row"><span>${esc(label)}</span><kbd>${esc(key)}</kbd></div>`).join('')}</div>
    <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Close')}</button></div>`);
}

export function installGlobalUi() {
  if (state.uiInstalled) return;
  state.uiInstalled = true;
  loadTabs();
  // The split workspace embeds this client in iframes. Tabs live in
  // localStorage, so react to writes from another frame instead of leaving a
  // stale tab bar behind.
  window.addEventListener('storage', (event) => {
    if (event.key !== 'nexus-tabs') return;
    loadTabs();
    renderTabs();
  });
  document.addEventListener('keydown', (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      if (state.palette) closePalette(); else openPalette();
    }
    const target = event.target;
    const typing = target instanceof HTMLElement && (target.closest('input, textarea, select, [contenteditable="true"]'));
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
    if (!typing && !event.ctrlKey && !event.metaKey && !event.altKey && event.key === '?') {
      event.preventDefault();
      openShortcutHelp();
    }
    if (!typing && (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'i') {
      const chatPage = document.getElementById('chat-page');
      if (chatPage) {
        event.preventDefault();
        chatPage.classList.toggle('context-open');
        storeSet('nexus-context-open', chatPage.classList.contains('context-open'));
      }
    }
    if (!typing && event.altKey && /^[1-9]$/.test(event.key)) {
      const tab = state.openTabs[Number(event.key) - 1];
      if (tab) {
        event.preventDefault();
        location.hash = tab.href || (tab.kind === 'session' ? `#/sessions/${tab.id}` : `#/runs/${tab.id}`);
      }
    }
    if (!typing && event.altKey && event.key.toLowerCase() === 'w') {
      const active = state.openTabs.find((tab) => (tab.href || (tab.kind === 'session' ? `#/sessions/${tab.id}` : `#/runs/${tab.id}`)) === location.hash);
      if (active) {
        event.preventDefault();
        closeTab(active.key || active.id);
      }
    }
    if (!typing && !event.ctrlKey && !event.metaKey && !event.altKey) {
      if (event.key.toLowerCase() === 'g') {
        goPending = true;
        clearTimeout(goTimer);
        goTimer = setTimeout(() => { goPending = false; }, 1200);
        return;
      }
      if (goPending) {
        const routes = {
          h: '#/', p: '#/projects', s: '#/sessions', a: '#/runs', u: '#/usage',
          f: '#/fanout', o: '#/hosts', ',': '#/settings',
        };
        const href = routes[event.key.toLowerCase()];
        goPending = false;
        clearTimeout(goTimer);
        if (href) {
          event.preventDefault();
          location.hash = href;
        }
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
