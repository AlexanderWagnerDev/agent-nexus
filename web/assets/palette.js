import { api } from './api.js';
import { state, storeGet, storeSet } from './state.js';
import { modal, modalRoot } from './ui.js';
import { newSessionPicker } from './components/new-session.js';
import { t } from './i18n.js';
import { svgIcon } from './icons.js';
import { agentMeta, esc, projectName } from './ui-format.js';

export function rememberRecent(entry) {
  const recent = storeGet('nexus-recent', []);
  const next = [entry, ...recent.filter((item) => item.id !== entry.id)].slice(0, 12);
  storeSet('nexus-recent', next);
}

export function paletteActions() {
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

export function paletteSession(s) {
  return {
    label: s.title || t('Untitled session'),
    hint: `${agentMeta(s.agent).label} · ${s.host_name} · ${projectName(s.cwd)}`,
    icon: 'sessions',
    run: () => { location.hash = `#/sessions/${s.id}`; },
  };
}

export function renderPaletteResults(items) {
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

export function closePalette() {
  modalRoot.innerHTML = '';
  state.palette = null;
}

export async function openPalette() {
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
