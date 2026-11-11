import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { agentGlyph, stateBadge, toast } from '../ui.js';
import { newSessionPicker } from '../components/new-session.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { agentMeta, countLabel, cwdKey, esc, fmtRelative, projectName } from '../ui-format.js';

export async function pageSessions() {
  renderShell('sessions');
  const params = new URLSearchParams(location.hash.split('?')[1] || '');
  const [{ hosts }] = await Promise.all([api('/hosts')]);
  const view = setPage(t('Sessions'), t('Discovered natively from each host; nothing is copied or rewritten.'), `<button class="btn primary small" id="sessions-new">${t('New session')}</button>`);
  view.innerHTML = `
    <div class="filterbar">
      <label class="search-field">${svgIcon('search')}<input class="input" id="filter-q" placeholder="${t('Filter title, project or path…')}" value="${esc(params.get('q') || '')}"></label>
      <select class="select" id="filter-agent" aria-label="${t('Filter by agent')}"><option value="">${t('All agents')}</option>${['claude', 'codex', 'opencode', 'gemini'].map((a) => `<option value="${a}" ${params.get('agent') === a ? 'selected' : ''}>${agentMeta(a).label}</option>`).join('')}</select>
      <select class="select" id="filter-host" aria-label="${t('Filter by host')}"><option value="">${t('All hosts')}</option>${hosts.map((h) => `<option value="${esc(h.id)}" ${params.get('host') === h.id ? 'selected' : ''}>${esc(h.name)}</option>`).join('')}</select>
      <select class="select" id="filter-state" aria-label="${t('Filter by state')}"><option value="">${t('Any state')}</option>${['running', 'waiting_for_input', 'idle', 'completed', 'failed', 'unknown'].map((s) => `<option value="${s}" ${params.get('state') === s ? 'selected' : ''}>${t(s.replaceAll('_', ' '))}</option>`).join('')}</select>
      <span class="filter-count" id="session-count"></span>
    </div>
    <div class="project-filter" id="project-filter" hidden></div>
    <div id="session-list"><div class="empty"><span class="spinner"></span></div></div>
    <div class="load-more" id="session-more"></div>`;
  document.getElementById('sessions-new').addEventListener('click', newSessionPicker);
  let offset = 0;
  let sessions = [];
  let hasMore = false;
  const project = params.get('project') || '';
  const clearProject = () => {
    const node = document.getElementById('project-filter');
    if (!node) return;
    node.hidden = !project;
    node.innerHTML = project ? `<span>${svgIcon('folder', 13)}${esc(projectName(project))}</span><code>${esc(project)}</code><button class="icon-btn" id="clear-project" type="button" aria-label="${t('Clear project filter')}">${svgIcon('x', 13)}</button>` : '';
    document.getElementById('clear-project')?.addEventListener('click', () => {
      const url = new URL(location.href);
      url.hash = '#/sessions';
      location.href = url.toString();
    });
  };
  const render = () => {
    const list = document.getElementById('session-list');
    if (!list) return;
    document.getElementById('session-count').textContent = t('{n} shown', { n: `${sessions.length}${hasMore ? '+' : ''}` });
    if (!sessions.length) {
      list.innerHTML = `<div class="card empty"><strong>${t('No sessions match')}</strong>${t('Try another filter, or wait for a host to reconnect.')}</div>`;
      document.getElementById('session-more').innerHTML = '';
      return;
    }
    const groups = new Map();
    for (const s of sessions) {
      const key = s.repo_url ? `repo:${s.repo_url.toLowerCase()}` : `dir:${s.host_id}|${cwdKey(s.cwd)}`;
      if (!groups.has(key)) groups.set(key, { name: projectName(s.cwd), cwd: s.cwd, hosts: new Set(), branches: new Set(), roots: new Set(), rows: [] });
      const group = groups.get(key);
      group.hosts.add(s.host_name || s.host_id);
      if (s.git_branch) group.branches.add(s.git_branch);
      if (s.repo_root) group.roots.add(cwdKey(s.repo_root));
      group.rows.push(s);
    }
    list.innerHTML = [...groups.values()].map((g) => `
      <section class="session-group">
        <div class="session-group-head">
          <span>${svgIcon('folder', 14)}<strong>${esc(g.name)}</strong><code>${esc(g.cwd)}</code></span>
          <span>${[...g.branches].slice(0, 3).map((b) => `<span class="branch-chip">${svgIcon('branch', 12)}${esc(b)}</span>`).join('')}${g.roots.size > 1 ? `<span class="muted">${t('{n} worktrees', { n: g.roots.size })}</span>` : ''}<span class="muted">${esc([...g.hosts].join(', '))} &middot; ${g.rows.length}</span></span>
        </div>
        <div class="session-list">${g.rows.map((s) => `
          <a class="session-row" href="#/sessions/${esc(s.id)}">
            ${agentGlyph(s.agent)}
            <span class="session-row-main"><strong>${esc(s.title || t('Untitled session'))}</strong><small>${g.hosts.size > 1 ? `${esc(s.host_name || s.host_id)} &middot; ` : ''}${esc(fmtRelative(s.updated_at))} &middot; ${countLabel(s.message_count || 0, 'message', 'messages')}${g.roots.size > 1 && s.repo_root ? ` &middot; ${esc(projectName(s.repo_root))}` : ''}</small></span>
            ${stateBadge(s.state)}
          </a>`).join('')}</div>
      </section>`).join('');
    document.getElementById('session-more').innerHTML = hasMore ? `<button class="btn" id="load-more-sessions">${t('Load more')}</button>` : '';
    document.getElementById('load-more-sessions')?.addEventListener('click', () => load(false).catch((e) => toast(e.message, true)));
  };
  const load = async (reset = true) => {
    if (reset) { offset = 0; sessions = []; document.getElementById('session-list').innerHTML = '<div class="empty"><span class="spinner"></span></div>'; }
    const q = new URLSearchParams();
    const query = document.getElementById('filter-q').value.trim();
    if (query) q.set('q', query);
    const agent = document.getElementById('filter-agent').value;
    if (agent) q.set('agent', agent);
    const host = document.getElementById('filter-host').value;
    if (host) q.set('host', host);
    const stateFilter = document.getElementById('filter-state').value;
    if (stateFilter) q.set('state', stateFilter);
    if (project) q.set('project', project);
    q.set('limit', '100');
    q.set('offset', String(offset));
    const data = await api('/sessions?' + q.toString());
    sessions = sessions.concat(data.sessions || []);
    offset += (data.sessions || []).length;
    hasMore = Boolean(data.has_more);
    const historyParams = new URLSearchParams(q);
    historyParams.delete('limit');
    historyParams.delete('offset');
    history.replaceState(null, '', '#/sessions' + (historyParams.toString() ? '?' + historyParams.toString() : ''));
    render();
  };
  let filterTimer = null;
  const schedule = () => { clearTimeout(filterTimer); filterTimer = setTimeout(() => load().catch((e) => toast(e.message, true)), 220); };
  document.getElementById('filter-q').addEventListener('input', schedule);
  for (const id of ['filter-agent', 'filter-host', 'filter-state']) document.getElementById(id).addEventListener('change', () => load().catch((e) => toast(e.message, true)));
  state.wsHandlers.add((msg) => { if (msg.type === 'sessions.changed') schedule(); });
  clearProject();
  await load();
}
