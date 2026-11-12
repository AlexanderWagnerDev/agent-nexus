import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { agentGlyph, stateBadge, toast } from '../ui.js';
import { newSessionPicker } from '../components/new-session.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { agentMeta, countLabel, cwdKey, esc, fmtRelative, nativeActivityBadge, projectName } from '../ui-format.js';

const ROW_HEIGHT = 56;
const GROUP_HEIGHT = 38;
const OVERSCAN_PX = 600;
const PAGE_SIZE = 200;
const LOAD_AHEAD_PX = 800;

function isoDaysAgo(days) {
  return new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();
}

function tagChips(tags) {
  return (tags || []).map((tag) => `<button class="tag-chip" type="button" data-tag-filter="${esc(tag)}" title="${t('Filter by tag')}">${esc(tag)}</button>`).join('');
}

function groupKey(session) {
  return session.repo_url ? `repo:${session.repo_url.toLowerCase()}` : `dir:${session.host_id}|${cwdKey(session.cwd)}`;
}

function groupSessions(sessions) {
  const groups = new Map();
  for (const session of sessions) {
    const key = groupKey(session);
    if (!groups.has(key)) {
      groups.set(key, {
        name: projectName(session.cwd), cwd: session.cwd,
        hosts: new Set(), branches: new Set(), roots: new Set(), rows: [],
      });
    }
    const group = groups.get(key);
    group.hosts.add(session.host_name || session.host_id);
    if (session.git_branch) group.branches.add(session.git_branch);
    if (session.repo_root) group.roots.add(cwdKey(session.repo_root));
    group.rows.push(session);
  }
  return [...groups.values()];
}

function groupHead(group) {
  return `<div class="session-group-head virtual-group">
    <span>${svgIcon('folder', 14)}<strong>${esc(group.name)}</strong><code>${esc(group.cwd)}</code></span>
    <span>${[...group.branches].slice(0, 3).map((b) => `<span class="branch-chip">${svgIcon('branch', 12)}${esc(b)}</span>`).join('')}${group.roots.size > 1 ? `<span class="muted">${t('{n} worktrees', { n: group.roots.size })}</span>` : ''}<span class="muted">${esc([...group.hosts].join(', '))} &middot; ${group.rows.length}</span></span>
  </div>`;
}

function row(session, group) {
  return `<a class="session-row virtual-row" href="#/sessions/${esc(session.id)}">
    ${agentGlyph(session.agent)}
    <span class="session-row-main"><strong>${esc(session.local_title || session.title || t('Untitled session'))}</strong><small>${group.hosts.size > 1 ? `${esc(session.host_name || session.host_id)} &middot; ` : ''}${esc(fmtRelative(session.updated_at))} &middot; ${countLabel(session.message_count || 0, 'message', 'messages')}${group.roots.size > 1 && session.repo_root ? ` &middot; ${esc(projectName(session.repo_root))}` : ''}</small></span>
    <span class="row-tags">${tagChips(session.tags)}</span>
    ${nativeActivityBadge(session.active_elsewhere, true)}
    ${stateBadge(session.state)}
  </a>`;
}

export async function pageSessions() {
  const pageGen = state.pageGen;
  renderShell('sessions');
  const params = new URLSearchParams(location.hash.split('?')[1] || '');
  const [hostData, tagData] = await Promise.all([api('/hosts'), api('/sessions/tags').catch(() => ({ tags: [] }))]);
  if (pageGen !== state.pageGen) return;
  const hosts = hostData.hosts || [];
  const tags = tagData.tags || [];
  const view = setPage(t('Sessions'), t('Discovered natively from each host; nothing is copied or rewritten.'), `<button class="btn primary small" id="sessions-new">${t('New session')}</button>`);
  const period = params.get('period') || 'any';
  view.innerHTML = `
    <div class="filterbar">
      <label class="search-field">${svgIcon('search')}<input class="input" id="filter-q" placeholder="${t('Filter title, project or path…')}" value="${esc(params.get('q') || '')}"></label>
      <select class="select" id="filter-agent" aria-label="${t('Filter by agent')}"><option value="">${t('All agents')}</option>${['claude', 'codex', 'opencode', 'gemini'].map((a) => `<option value="${a}" ${params.get('agent') === a ? 'selected' : ''}>${agentMeta(a).label}</option>`).join('')}</select>
      <select class="select" id="filter-host" aria-label="${t('Filter by host')}"><option value="">${t('All hosts')}</option>${hosts.map((h) => `<option value="${esc(h.id)}" ${params.get('host') === h.id ? 'selected' : ''}>${esc(h.name)}</option>`).join('')}</select>
      <select class="select" id="filter-state" aria-label="${t('Filter by state')}"><option value="">${t('Any state')}</option>${['running', 'waiting_for_input', 'idle', 'completed', 'failed', 'unknown'].map((s) => `<option value="${s}" ${params.get('state') === s ? 'selected' : ''}>${t(s.replaceAll('_', ' '))}</option>`).join('')}</select>
      <select class="select" id="filter-tag" aria-label="${t('Filter by tag')}"><option value="">${t('All tags')}</option>${(tags || []).map((tag) => `<option value="${esc(tag)}" ${params.get('tag') === tag ? 'selected' : ''}>${esc(tag)}</option>`).join('')}</select>
      <select class="select" id="filter-period" aria-label="${t('Time range')}">
        ${[['any', t('Any time')], ['24h', t('Last 24 hours')], ['7d', t('Last 7 days')], ['30d', t('Last 30 days')], ['custom', t('Custom range')]].map(([value, label]) => `<option value="${value}" ${period === value ? 'selected' : ''}>${label}</option>`).join('')}
      </select>
      <select class="select" id="filter-sort" aria-label="${t('Sort by')}">
        ${[['updated', t('updated')], ['created', t('created')], ['title', t('title')], ['messages', t('messages')]].map(([value, label]) => `<option value="${value}" ${(params.get('sort') || 'updated') === value ? 'selected' : ''}>${label}</option>`).join('')}
      </select>
      <select class="select" id="filter-order" aria-label="${t('Order')}">
        <option value="desc" ${(params.get('order') || 'desc') === 'desc' ? 'selected' : ''}>${t('Newest first')}</option>
        <option value="asc" ${params.get('order') === 'asc' ? 'selected' : ''}>${t('Oldest first')}</option>
      </select>
      <span class="filter-count" id="session-count" role="status" aria-live="polite"></span>
    </div>
    <div class="filterbar" id="custom-range" ${period === 'custom' ? '' : 'hidden'}>
      <label class="label-inline">${t('From')}<input class="input" type="datetime-local" id="filter-since" value="${esc(params.get('since_local') || '')}"></label>
      <label class="label-inline">${t('To')}<input class="input" type="datetime-local" id="filter-until" value="${esc(params.get('until_local') || '')}"></label>
    </div>
    <div class="project-filter" id="project-filter" hidden></div>
    <div class="session-virtual" id="session-list" tabindex="0" aria-label="${t('Sessions')}">
      <div class="session-virtual-spacer" id="session-spacer"></div>
      <div class="session-virtual-window" id="session-window"></div>
      <div class="session-sentinel" id="session-sentinel"></div>
    </div>`;
  document.getElementById('sessions-new').addEventListener('click', newSessionPicker);

  let sessions = [];
  let items = [];
  let tops = [];
  let heights = [];
  let totalHeight = 0;
  let total = 0;
  let offset = 0;
  let hasMore = false;
  let loading = false;
  let lastStart = -1;
  let lastEnd = -1;
  let scrollFrame = null;
  let pendingReset = false;
  const project = params.get('project') || '';

  const list = document.getElementById('session-list');
  const windowEl = document.getElementById('session-window');
  const spacer = document.getElementById('session-spacer');
  const sentinel = document.getElementById('session-sentinel');

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

  const rebuild = () => {
    const groups = groupSessions(sessions);
    items = [];
    for (const group of groups) {
      items.push({ kind: 'group', group });
      for (const session of group.rows) items.push({ kind: 'row', session, group });
    }
    heights = items.map((item) => item.kind === 'group' ? GROUP_HEIGHT : ROW_HEIGHT);
    tops = new Array(items.length);
    let y = 0;
    for (let i = 0; i < items.length; i++) {
      tops[i] = y;
      y += heights[i];
    }
    totalHeight = y;
    spacer.style.height = `${totalHeight}px`;
    sentinel.style.top = `${totalHeight}px`;
    lastStart = -1;
    lastEnd = -1;
  };

  const lowerBound = (value) => {
    let lo = 0;
    let hi = items.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (tops[mid] < value) lo = mid + 1;
      else hi = mid;
    }
    return lo;
  };

  const updateCount = () => {
    const count = document.getElementById('session-count');
    if (!count) return;
    if (loading && !sessions.length) {
      count.textContent = t('Loading…');
      return;
    }
    const shown = sessions.length;
    if (!total && !hasMore) count.textContent = t('{n} shown', { n: shown });
    else count.textContent = t('{shown} of {total}', { shown, total: Math.max(total, shown) });
  };

  const renderWindow = () => {
    if (!items.length) {
      windowEl.innerHTML = '';
      return;
    }
    const height = list.clientHeight || 0;
    if (!height) {
      requestAnimationFrame(renderWindow);
      return;
    }
    const start = Math.max(0, lowerBound(Math.max(0, list.scrollTop - OVERSCAN_PX)));
    const end = Math.min(items.length, lowerBound(list.scrollTop + height + OVERSCAN_PX) + 1);
    if (start === lastStart && end === lastEnd) return;
    lastStart = start;
    lastEnd = end;
    windowEl.style.transform = `translateY(${tops[start]}px)`;
    let html = '';
    for (let i = start; i < end; i++) {
      const item = items[i];
      html += item.kind === 'group' ? groupHead(item.group) : row(item.session, item.group);
    }
    windowEl.innerHTML = html;
  };

  const maybeLoadMore = () => {
    if (!hasMore || loading) return;
    if (list.scrollTop + list.clientHeight >= totalHeight - LOAD_AHEAD_PX) {
      load(false).catch((err) => toast(err.message, true));
    }
  };

  const onScroll = () => {
    if (scrollFrame) return;
    scrollFrame = requestAnimationFrame(() => {
      scrollFrame = null;
      renderWindow();
      maybeLoadMore();
    });
  };
  list.addEventListener('scroll', onScroll, { passive: true });
  window.addEventListener('resize', () => {
    lastStart = -1;
    lastEnd = -1;
    renderWindow();
  });

  list.addEventListener('click', (event) => {
    const button = event.target.closest('[data-tag-filter]');
    if (!button) return;
    event.preventDefault();
    document.getElementById('filter-tag').value = button.dataset.tagFilter;
    load(true).catch((err) => toast(err.message, true));
  });

  const load = async (reset = true) => {
    if (pageGen !== state.pageGen) return;
    if (loading) {
      if (reset) pendingReset = true;
      return;
    }
    loading = true;
    if (reset) {
      sessions = [];
      offset = 0;
      total = 0;
      hasMore = false;
      items = [];
      lastStart = -1;
      lastEnd = -1;
      list.scrollTop = 0;
      windowEl.innerHTML = '';
      windowEl.style.transform = 'translateY(0)';
      spacer.style.height = '0px';
      sentinel.style.top = '0px';
    }
    updateCount();
    try {
      const q = new URLSearchParams();
      const query = document.getElementById('filter-q').value.trim();
      if (query) q.set('q', query);
      const agent = document.getElementById('filter-agent').value;
      if (agent) q.set('agent', agent);
      const host = document.getElementById('filter-host').value;
      if (host) q.set('host', host);
      const stateFilter = document.getElementById('filter-state').value;
      if (stateFilter) q.set('state', stateFilter);
      const tag = document.getElementById('filter-tag').value;
      if (tag) q.set('tag', tag);
      if (project) q.set('project', project);
      const period = document.getElementById('filter-period').value;
      if (period === '24h') q.set('since', isoDaysAgo(1));
      if (period === '7d') q.set('since', isoDaysAgo(7));
      if (period === '30d') q.set('since', isoDaysAgo(30));
      let sinceLocal = '', untilLocal = '';
      if (period === 'custom') {
        sinceLocal = document.getElementById('filter-since').value;
        untilLocal = document.getElementById('filter-until').value;
        if (sinceLocal) q.set('since', new Date(sinceLocal).toISOString());
        if (untilLocal) q.set('until', new Date(untilLocal).toISOString());
      }
      const sort = document.getElementById('filter-sort').value;
      if (sort) q.set('sort', sort);
      const order = document.getElementById('filter-order').value;
      if (order) q.set('order', order);
      q.set('limit', String(PAGE_SIZE));
      q.set('offset', String(offset));
      const data = await api('/sessions?' + q.toString());
      if (pageGen !== state.pageGen) return;
      const page = data.sessions || [];
      const seen = new Set(sessions.map((s) => s.id));
      sessions = sessions.concat(page.filter((s) => !seen.has(s.id)));
      offset = (data.offset || 0) + page.length;
      hasMore = Boolean(data.has_more);
      total = Number(data.total) || sessions.length;
      const historyParams = new URLSearchParams(q);
      historyParams.delete('limit');
      historyParams.delete('offset');
      if (period !== 'any') historyParams.set('period', period);
      if (period === 'custom') {
        if (sinceLocal) historyParams.set('since_local', sinceLocal);
        if (untilLocal) historyParams.set('until_local', untilLocal);
        historyParams.delete('since');
        historyParams.delete('until');
      }
      history.replaceState(null, '', '#/sessions' + (historyParams.toString() ? '?' + historyParams.toString() : ''));
      rebuild();
      if (!sessions.length) {
        windowEl.style.transform = 'translateY(0)';
        windowEl.innerHTML = `<div class="card empty"><strong>${t('No sessions match')}</strong>${t('Try another filter, or wait for a host to reconnect.')}</div>`;
        lastStart = -1;
        lastEnd = -1;
      } else {
        renderWindow();
      }
      updateCount();
    } finally {
      loading = false;
      updateCount();
      if (pendingReset) {
        pendingReset = false;
        setTimeout(() => load(true).catch((err) => toast(err.message, true)), 0);
      }
    }
  };

  // Load the next page before the reader reaches the bottom, without polling.
  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      if (hasMore && !loading) load(false).catch((err) => toast(err.message, true));
    }
  }, { root: list, rootMargin: `${LOAD_AHEAD_PX}px 0px` });
  observer.observe(sentinel);

  let filterTimer = null;
  const schedule = () => { clearTimeout(filterTimer); filterTimer = setTimeout(() => load(true).catch((err) => toast(err.message, true)), 220); };
  document.getElementById('filter-q').addEventListener('input', schedule);
  for (const id of ['filter-agent', 'filter-host', 'filter-state', 'filter-tag', 'filter-sort', 'filter-order']) document.getElementById(id).addEventListener('change', () => load(true).catch((err) => toast(err.message, true)));
  document.getElementById('filter-period').addEventListener('change', () => {
    const custom = document.getElementById('filter-period').value === 'custom';
    document.getElementById('custom-range').hidden = !custom;
    load(true).catch((err) => toast(err.message, true));
  });
  for (const id of ['filter-since', 'filter-until']) document.getElementById(id).addEventListener('change', () => load(true).catch((err) => toast(err.message, true)));
  state.wsHandlers.add((msg) => { if (msg.type === 'sessions.changed') schedule(); });
  state.pageCleanup = () => observer.disconnect();
  clearProject();
  await load(true);
}
