/* DOM, modal and toast primitives shared by every view. */
import { t } from './i18n.js';
import { agentMeta, esc } from './ui-format.js';

export const app = document.getElementById('app');

export const modalRoot = document.getElementById('modal-root');

export const toastRoot = document.getElementById('toast-root');

export let modalOnClose = null;

export function agentGlyph(agent, size = 'normal') {
  const meta = agentMeta(agent);
  return `<span class="agent-glyph ${size}" data-agent="${esc(String(agent || 'agent').toLowerCase())}" title="${esc(meta.label)}">${esc(meta.short)}</span>`;
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
  navigator.clipboard?.writeText(text).then(() => toast('Copied to clipboard')).catch(() => toast('Could not copy', true));
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
  modalOnClose = onClose || null;
  modalRoot.innerHTML = `<div class="modal-backdrop" data-modal-backdrop><div class="modal" role="dialog" aria-modal="true">${html}</div></div>`;
  const backdrop = modalRoot.firstElementChild;
  backdrop.addEventListener('click', (e) => {
    if (e.target === backdrop) closeModal();
  });
  modalRoot.querySelectorAll('[data-close]').forEach((b) => b.addEventListener('click', closeModal));
}

export function closeModal() {
  const cb = modalOnClose;
  modalOnClose = null;
  modalRoot.innerHTML = '';
  if (cb) cb();
}

export function confirmDanger(message, onConfirm) {
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
