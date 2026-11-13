import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { agentBadge, toast } from '../ui.js';
import { t } from '../i18n.js';
import { esc, fmtRelative } from '../ui-format.js';

export async function pageSearch() {
  const pageGen = state.pageGen;
  renderShell('search');
  const params = new URLSearchParams(location.hash.split('?')[1] || '');
  const initial = params.get('q') || '';
  const view = setPage(t('Search'), t('Federated transcript search runs on each online host. Content is never indexed on the hub.'));
  view.innerHTML = `
    <form class="toolbar" id="search-form">
      <input class="input grow" id="search-q" placeholder="${t('Search all transcripts, e.g. rate limiter')}" value="${esc(initial)}" autofocus>
      <select class="select" id="search-agent" aria-label="${t('Filter by agent')}"><option value="">${t('All agents')}</option></select>
      <select class="select" id="search-host" aria-label="${t('Filter by host')}"><option value="">${t('All hosts')}</option></select>
      <button class="btn primary" type="submit">${t('Search')}</button>
    </form>
    <div id="search-results">${initial ? '<div class="empty"><span class="spinner"></span></div>' : `<div class="card empty"><strong>${t('Search across hosts')}</strong>${t('Results come from the agent stores; offline hosts are reported, not silently skipped.')}</div>`}</div>`;
  let data = { results: [], not_searched: {} };
  const render = () => {
    const box = document.getElementById('search-results');
    if (!box) return;
    const agent = document.getElementById('search-agent').value;
    const host = document.getElementById('search-host').value;
    const results = data.results.filter((r) => (!agent || r.agent === agent) && (!host || r.host_name === host));
    const skipped = Object.keys(data.not_searched || {});
    const building = Object.entries(data.index_status || {})
      .filter(([, status]) => status.state === 'building')
      .map(([name, status]) => {
        const pct = status.total ? Math.min(100, Math.floor((status.indexed * 100) / status.total)) : 0;
        return t('Building the search index on {host}: {n}%', { host: name, n: pct });
      });
    box.innerHTML = `
      ${building.length ? `<div class="notice info mb-2">${building.map(esc).join(' / ')}. ${t('Using the slower adapter search until the index is ready.')}</div>` : ''}
      ${skipped.length ? `<div class="notice info mb-2">${t('Not searched:')} ${skipped.map(esc).join(', ')}</div>` : ''}
      ${results.length ? `<div class="list">${results.map((r) => {
        const inner = `<div style="min-width:0"><div class="title">${esc(r.title || t('Untitled session'))}</div><div class="sub"><span>${esc(r.snippet)}</span></div></div><div class="right">${agentBadge(r.agent)}${r.host_name ? `<span class="small muted">${esc(r.host_name)}</span>` : ''}<span class="small muted">${fmtRelative(r.at)}</span></div>`;
        return r.session_id
          ? `<a class="list-row" href="#/sessions/${esc(r.session_id)}">${inner}</a>`
          : `<div class="list-row" style="cursor:default">${inner}</div>`;
      }).join('')}</div>` : `<div class="card empty"><strong>${t('No matches')}</strong>${t('Try another term, another filter, or check that hosts are online.')}</div>`}`;
  };
  const run = async (query) => {
    if (query.length < 2) return;
    history.replaceState(null, '', `#/search?q=${encodeURIComponent(query)}`);
    const box = document.getElementById('search-results');
    box.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
    data = await api('/search?q=' + encodeURIComponent(query));
    if (pageGen !== state.pageGen) return;
    const agents = [...new Set((data.results || []).map((r) => r.agent).filter(Boolean))].sort();
    const hosts = [...new Set((data.results || []).map((r) => r.host_name).filter(Boolean))].sort();
    const agentSelect = document.getElementById('search-agent');
    const hostSelect = document.getElementById('search-host');
    const agentValue = agentSelect.value;
    const hostValue = hostSelect.value;
    agentSelect.innerHTML = `<option value="">${t('All agents')}</option>` + agents.map((a) => `<option value="${esc(a)}">${esc(a)}</option>`).join('');
    hostSelect.innerHTML = `<option value="">${t('All hosts')}</option>` + hosts.map((h) => `<option value="${esc(h)}">${esc(h)}</option>`).join('');
    agentSelect.value = agents.includes(agentValue) ? agentValue : '';
    hostSelect.value = hosts.includes(hostValue) ? hostValue : '';
    render();
  };
  document.getElementById('search-agent').addEventListener('change', render);
  document.getElementById('search-host').addEventListener('change', render);
  document.getElementById('search-form').addEventListener('submit', (e) => {
    e.preventDefault();
    run(document.getElementById('search-q').value.trim()).catch((err) => toast(err.message, true));
  });
  if (initial) run(initial).catch((err) => toast(err.message, true));
}
