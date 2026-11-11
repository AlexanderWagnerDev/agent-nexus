import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { state, storeGet, storeSet } from '../state.js';
import { agentGlyph, badge, closeModal, modal, modalRoot, stateBadge, toast } from '../ui.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { countLabel, esc, projectName } from '../ui-format.js';

export async function pageWorkspace() {
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
