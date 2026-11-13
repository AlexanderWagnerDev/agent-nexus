import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { agentGlyph, stateBadge, toast } from '../ui.js';
import { newSessionPicker } from '../components/new-session.js';
import { t, pluralNoun } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { countLabel, cwdKey, esc, fmtDuration, projectName } from '../ui-format.js';

export async function pageRuns() {
  const pageGen = state.pageGen;
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
      <div><strong>${runs.length}</strong><span>${pluralNoun(runs.length, 'open run', 'open runs')}</span></div>
      <div><strong>${running}</strong><span>${t('working')}</span></div>
      <div><strong class="${waiting ? 'warn' : ''}">${waiting}</strong><span>${t('waiting for you')}</span></div>
      <div><strong>${new Set(runs.map((r) => r.host_id)).size}</strong><span>${pluralNoun(new Set(runs.map((r) => r.host_id)).size, 'host', 'hosts')}</span></div>`;
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
    if (pageGen !== state.pageGen) return;
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
