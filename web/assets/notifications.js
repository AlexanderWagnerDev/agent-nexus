import { api } from './api.js';
import { state, storeGet, storeSet } from './state.js';
import { badge } from './ui.js';
import { t } from './i18n.js';
import { svgIcon } from './icons.js';
import { esc, fmtRelative } from './ui-format.js';

export function notificationIcon(kind) {
  return svgIcon({ approval: 'alert', failed: 'alert', disconnected: 'hosts', completed: 'check' }[kind] || 'bell');
}

export async function loadNotifications() {
  const data = await api('/notifications?limit=40');
  state.notifications = data.notifications || [];
  state.unread = data.unread || 0;
  updateNotificationBadge();
  const panel = document.getElementById('notif-panel');
  if (panel && !panel.hidden) renderNotificationPanel();
}

export function updateNotificationBadge() {
  const badgeNode = document.getElementById('notif-badge');
  if (!badgeNode) return;
  badgeNode.textContent = state.unread > 99 ? '99+' : String(state.unread || '');
  badgeNode.classList.toggle('visible', state.unread > 0);
  const toggle = document.getElementById('notif-toggle');
  toggle?.classList.toggle('has-unread', state.unread > 0);
  toggle?.setAttribute('aria-label', state.unread ? t('Notifications, {n} unread', { n: state.unread }) : t('Notifications'));
}

export function renderNotificationPanel() {
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

export async function toggleNotifications() {
  const panel = document.getElementById('notif-panel');
  if (!panel) return;
  if (!panel.hidden) { panel.hidden = true; return; }
  panel.hidden = false;
  renderNotificationPanel();
  try { await loadNotifications(); } catch { /* keep cached list */ }
}
