/* DOM, modal and toast primitives shared by every view. */
import { t } from './i18n.js';
import { agentLogo } from './icons.js';
import { agentMeta, esc } from './ui-format.js';

const doc = typeof document !== 'undefined' ? document : null;

export const app = doc?.getElementById('app') || null;

export const modalRoot = doc?.getElementById('modal-root') || null;

export const toastRoot = doc?.getElementById('toast-root') || null;

export let modalOnClose = null;
let modalReturnFocus = null;

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function focusableWithin(root) {
  return [...root.querySelectorAll(FOCUSABLE)].filter((node) => node.getClientRects().length > 0);
}

/** Keep Tab/Shift+Tab inside a dialog and return focus on close. */
export function trapTabKey(event, root) {
  if (event.key !== 'Tab') return;
  const items = focusableWithin(root);
  if (!items.length) return;
  const first = items[0];
  const last = items[items.length - 1];
  if (!root.contains(document.activeElement)) {
    // A re-render inside the dialog can drop focus to <body>; keep the trap
    // closed instead of letting Tab walk into the page behind it.
    event.preventDefault();
    (event.shiftKey ? last : first).focus();
    return;
  }
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

export function agentGlyph(agent, size = 'normal') {
  const meta = agentMeta(agent);
  const label = esc(meta.label);
  return `<span class="agent-glyph ${size}" data-agent="${esc(String(agent || 'agent').toLowerCase())}" role="img" aria-label="${label}" title="${label}">${agentLogo(agent) || esc(meta.short)}</span>`;
}

export function badge(text, cls = '') {
  return `<span class="badge ${esc(cls)}">${esc(text)}</span>`;
}

export function stateBadge(s) {
  const map = {
    running: 'running', waiting_for_input: 'waiting_for_input', idle: 'idle',
    completed: 'completed', failed: 'failed', unknown: 'unknown',
    starting: 'running', stopping: 'waiting_for_input', exited: 'completed',
  };
  return badge(t((s || 'unknown').replaceAll('_', ' ')), map[s] || 'unknown');
}

export function agentBadge(agent) {
  return `<span class="badge agent no-dot">${esc(agent || 'agent')}</span>`;
}

export function toast(message, isError = false) {
  const node = document.createElement('div');
  node.className = `toast ${isError ? 'error' : ''}`;
  node.textContent = message;
  toastRoot.appendChild(node);
  setTimeout(() => node.remove(), 4200);
}

export function copyText(text) {
  navigator.clipboard?.writeText(text).then(() => toast(t('Copied to clipboard'))).catch(() => toast(t('Could not copy'), true));
}

export function b64encode(bytes) {
  let s = '';
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s);
}

export function b64decode(value) {
  const bin = atob(value);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function modal(html, onClose) {
  if (modalRoot.firstElementChild) closeModal();
  modalReturnFocus = document.activeElement;
  modalOnClose = onClose || null;
  modalRoot.innerHTML = `<div class="modal-backdrop" data-modal-backdrop><div class="modal" role="dialog" aria-modal="true" tabindex="-1">${html}</div></div>`;
  const dialog = modalRoot.querySelector('.modal');
  const heading = dialog.querySelector('h1, h2, h3');
  if (heading) {
    if (!heading.id) heading.id = 'modal-heading';
    dialog.setAttribute('aria-labelledby', heading.id);
  }
  app?.setAttribute('inert', '');
  const backdrop = modalRoot.firstElementChild;
  backdrop.addEventListener('click', (e) => {
    if (e.target === backdrop) closeModal();
  });
  backdrop.addEventListener('keydown', (event) => trapTabKey(event, backdrop));
  modalRoot.querySelectorAll('[data-close]').forEach((b) => b.addEventListener('click', closeModal));
  const first = focusableWithin(backdrop)[0];
  (first || dialog)?.focus();
}

export function closeModal() {
  const cb = modalOnClose;
  modalOnClose = null;
  modalRoot.innerHTML = '';
  app?.removeAttribute('inert');
  const target = modalReturnFocus;
  modalReturnFocus = null;
  if (target && target.isConnected) target.focus();
  if (cb) cb();
}

export function confirmDanger(message, onConfirm) {
  modal(`
    <h2>${t('Are you sure?')}</h2>
    <p class="hint">${esc(message)}</p>
    <div class="modal-actions">
      <button class="btn ghost" type="button" data-close>${t('Cancel')}</button>
      <button class="btn danger" type="button" id="confirm-danger">${t('Confirm')}</button>
    </div>`);
  document.getElementById('confirm-danger').addEventListener('click', async () => {
    closeModal();
    await onConfirm();
  });
}
