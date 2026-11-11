import { state, storeGet, storeSet } from './state.js';
import { agentGlyph } from './ui.js';
import { svgIcon } from './icons.js';
import { esc } from './ui-format.js';

export function loadTabs() {
  state.openTabs = storeGet('nexus-tabs', []);
}

export function saveTabs() {
  storeSet('nexus-tabs', state.openTabs);
  renderTabs();
}

export function openTab(entry) {
  if (!entry?.id) return;
  const key = entry.key || entry.id;
  entry.key = key;
  state.openTabs = [entry, ...state.openTabs.filter((tab) => (tab.key || tab.id) !== key)].slice(0, 8);
  saveTabs();
}

export function closeTab(key) {
  state.openTabs = state.openTabs.filter((tab) => (tab.key || tab.id) !== key);
  saveTabs();
}

export function renderTabs() {
  const bar = document.getElementById('tabs-bar');
  if (!bar) return;
  if (state.embed || !state.openTabs.length) {
    bar.innerHTML = '';
    bar.hidden = true;
    return;
  }
  bar.hidden = false;
  const seenHrefs = new Set();
  const tabs = state.openTabs.filter((tab) => {
    const href = tab.href || (tab.kind === 'session' ? `#/sessions/${tab.id}` : `#/runs/${tab.id}`);
    if (seenHrefs.has(href)) return false;
    seenHrefs.add(href);
    return true;
  });
  bar.innerHTML = `<div class="tabs-scroll">${tabs.map((tab) => {
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
