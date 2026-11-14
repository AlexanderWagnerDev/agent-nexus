import { state, storeGet, storeSet } from './state.js';
import { agentGlyph } from './ui.js';
import { svgIcon } from './icons.js';
import { esc } from './ui-format.js';
import { t } from './i18n.js';

export function loadTabs() {
  state.openTabs = storeGet('nexus-tabs', []);
}

export function saveTabs() {
  storeSet('nexus-tabs', state.openTabs);
  renderTabs();
}

/**
 * A tab is unique per native session, not per URL. Run and session views for
 * the same session therefore share one tab instead of adding a second label.
 * Pure so the dedupe rule is covered by unit tests.
 */
export function mergeTabs(tabs, entry) {
  const key = entry.key || entry.id;
  const sessionKey = entry.session_id || (entry.kind === 'session' ? entry.id : '');
  const next = { ...entry, key };
  const kept = tabs.filter((tab) => {
    if ((tab.key || tab.id) === key) return false;
    if (sessionKey && (tab.session_id === sessionKey || (tab.kind === 'session' && tab.id === sessionKey))) return false;
    return true;
  });
  return [next, ...kept].slice(0, 8);
}

export function openTab(entry) {
  if (!entry?.id || state.embed) return;
  state.openTabs = mergeTabs(state.openTabs, entry);
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
  const seen = new Set();
  const tabs = state.openTabs.filter((tab) => {
    const key = tab.session_id || tab.key || tab.id;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
  bar.innerHTML = `<div class="tabs-scroll">${tabs.map((tab) => {
    const key = tab.key || tab.id;
    const href = tab.href || (tab.kind === 'session' ? `#/sessions/${esc(tab.id)}` : `#/runs/${esc(tab.id)}`);
    const active = location.hash === href;
    return `<a class="tab ${active ? 'active' : ''}" href="${esc(href)}" title="${esc(tab.title || '')}">
      ${agentGlyph(tab.agent, 'tiny')}<span class="tab-title">${esc(tab.title || t('Untitled'))}</span>
      <button class="tab-close" type="button" data-close-tab="${esc(key)}" aria-label="${t('Close tab')}">${svgIcon('x', 13)}</button>
    </a>`;
  }).join('')}</div>`;
  bar.querySelectorAll('[data-close-tab]').forEach((button) => button.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    closeTab(button.dataset.closeTab);
  }));
}
