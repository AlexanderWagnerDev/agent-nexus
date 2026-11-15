/* Hash router with a registered page matcher. */

import { state } from './state.js';
import { esc } from './ui-format.js';
import { renderTabs } from './tabs.js';
import { parseRoute } from './routes.js';
import { closeModal } from './ui.js';

let pageHandler = null;

/** Register the function that renders a page for the current hash. */
export function setPageHandler(fn) {
  pageHandler = fn;
}

export async function route() {
  if (!state.me) return;
  // A dialog is mounted outside #app and makes #app inert. Leaving it open
  // across a route change would present the old page's dialog on top of an
  // unclickable new page.
  closeModal();
  state.pageGen += 1;
  if (state.pageCleanup) {
    try { state.pageCleanup(); } catch { /* ignore */ }
    state.pageCleanup = null;
  }
  state.wsHandlers.clear();
  state.run = null;
  const view = document.getElementById('view');
  if (view) view.className = 'view';
  try {
    return await pageHandler?.();
  } catch (err) {
    if (err.status === 401) return;
    console.error('route failed', err);
    const target = document.getElementById('view');
    if (target) target.innerHTML = `<div class="notice error">${esc(err.message)}</div>`;
  } finally {
    renderTabs();
  }
}
