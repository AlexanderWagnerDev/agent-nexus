import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { agentMeta, countLabel, esc, fmtStamp } from '../ui-format.js';
import { t } from '../i18n.js';

function money(value, runs) {
  if (!runs) return '—';
  return `$${Number(value || 0).toFixed(4)}`;
}

function tokens(value) {
  return Number(value || 0).toLocaleString();
}

function groupTable(title, rows, labelKey) {
  if (!rows.length) return `<section class="card"><h2 class="mt-0">${esc(title)}</h2><p class="muted">${t('No usage data for this period.')}</p></section>`;
  return `<section class="card">
    <h2 class="mt-0">${esc(title)}</h2>
    <div class="table-wrap" tabindex="0" role="region" aria-label="${t('Usage table')}"><table class="table">
      <thead><tr><th>${t(labelKey)}</th><th>${t('Input')}</th><th>${t('Output')}</th><th>${t('Cache read')}</th><th>${t('Cache write')}</th><th>${t('Cost')}</th><th>${t('Runs')}</th></tr></thead>
      <tbody>${rows.map((row) => `<tr><td>${esc(row.name)}</td><td>${tokens(row.input_tokens)}</td><td>${tokens(row.output_tokens)}</td><td>${tokens(row.cache_read_tokens)}</td><td>${tokens(row.cache_write_tokens)}</td><td>${row.runs_with_usage ? `$${Number(row.cost_usd).toFixed(4)}` : '—'}</td><td>${row.runs_with_usage}/${row.runs}</td></tr>`).join('')}</tbody>
    </table></div>
  </section>`;
}

export async function pageUsage() {
  const pageGen = state.pageGen;
  renderShell('usage');
  const params = new URLSearchParams(location.hash.split('?')[1] || '');
  const period = params.get('period') || '30';
  const hostFilter = params.get('host') || '';
  const agentFilter = params.get('agent') || '';
  const view = setPage(t('Usage'), t('Token and cost metadata reported by your agents. Nothing is estimated and no chat content is stored.'));
  view.innerHTML = `<div class="empty"><span class="spinner"></span></div>`;

  const [{ hosts }, report] = await Promise.all([
    api('/hosts').catch(() => ({ hosts: [] })),
    api(`/usage?${new URLSearchParams({
      since: new Date(Date.now() - Number(period) * 86400000).toISOString(),
      until: new Date().toISOString(),
      host: hostFilter,
      agent: agentFilter,
    })}`),
  ]);
  if (pageGen !== state.pageGen) return;

  const summary = report.summary || {};
  const daily = report.daily || [];
  const maxDaily = Math.max(1, ...daily.map((day) => (day.input_tokens || 0) + (day.output_tokens || 0)));
  const hasDailyTokens = daily.some((day) => (day.input_tokens || 0) + (day.output_tokens || 0) > 0);
  const cards = [
    [t('Input tokens'), tokens(summary.input_tokens)],
    [t('Output tokens'), tokens(summary.output_tokens)],
    [t('Cache read'), tokens(summary.cache_read_tokens)],
    [t('Cache write'), tokens(summary.cache_write_tokens)],
    [t('Reported cost (USD)'), money(summary.cost_usd, summary.cost_runs)],
    [t('Runs with usage'), `${summary.runs_with_usage || 0} / ${summary.runs || 0}`],
  ];
  const agentOptions = ['', 'claude', 'codex', 'opencode', 'gemini'];
  view.innerHTML = `
    <div class="filterbar">
      <label class="label-inline">${t('Period')}<select class="select" id="usage-period">
        ${[['7', t('Last 7 days')], ['30', t('Last 30 days')], ['90', t('Last 90 days')], ['365', t('Last 12 months')]].map(([value, label]) => `<option value="${value}" ${period === value ? 'selected' : ''}>${label}</option>`).join('')}
      </select></label>
      <label class="label-inline">${t('Host')}<select class="select" id="usage-host"><option value="">${t('All hosts')}</option>${(hosts || []).map((host) => `<option value="${esc(host.id)}" ${hostFilter === host.id ? 'selected' : ''}>${esc(host.name)}</option>`).join('')}</select></label>
      <label class="label-inline">${t('Agent')}<select class="select" id="usage-agent"><option value="">${t('All agents')}</option>${agentOptions.filter(Boolean).map((agent) => `<option value="${agent}" ${agentFilter === agent ? 'selected' : ''}>${agentMeta(agent).label}</option>`).join('')}</select></label>
      <span class="filter-count">${countLabel(summary.runs || 0, 'run', 'runs')}</span>
    </div>
    <div class="usage-cards">
      ${cards.map(([label, value]) => `<div class="card usage-card"><span class="aside-label">${esc(label)}</span><strong>${esc(value)}</strong></div>`).join('')}
    </div>
    <div class="notice info small">${t('Only values reported by the agent runtime are shown. Costs are never inferred from public price lists; a missing cost is marked with a dash.')} ${t('{n} runs had no usage metadata.', { n: summary.unavailable_runs || 0 })}</div>
    <section class="card">
      <div class="split"><h2 class="mt-0">${t('Daily activity')}</h2><span class="muted small">${t('Input + output tokens')}</span></div>
      ${hasDailyTokens ? `<div class="usage-chart" role="img" aria-label="${t('Daily token usage')}">${daily.map((day) => {
        const total = (day.input_tokens || 0) + (day.output_tokens || 0);
        const height = Math.max(2, Math.round((total / maxDaily) * 100));
        return `<div class="usage-bar" title="${esc(day.date)}: ${tokens(total)} tokens · ${countLabel(day.runs, 'run', 'runs')}"><span style="height:${height}%"></span><small>${esc(day.date.slice(5))}</small></div>`;
      }).join('')}</div>` : `<p class="muted">${t('No usage data for this period.')}</p>`}
    </section>
    <div class="grid cols-2">
      ${groupTable(t('By agent'), report.by_agent || [], 'Agent')}
      ${groupTable(t('By host'), report.by_host || [], 'Host')}
    </div>
    ${groupTable(t('By project'), report.by_project || [], 'Project')}
    ${groupTable(t('By session'), report.by_session || [], 'Session')}
    <section class="card">
      <h2 class="mt-0">${t('Recent runs')}</h2>
      ${(report.runs || []).length ? `<div class="table-wrap" tabindex="0" role="region" aria-label="${t('Usage table')}"><table class="table"><thead><tr><th>${t('Session')}</th><th>${t('Host')}</th><th>${t('Agent')}</th><th>${t('Started')}</th><th>${t('Input')}</th><th>${t('Output')}</th><th>${t('Cost')}</th><th>${t('Source')}</th></tr></thead><tbody>
        ${report.runs.map((run) => `<tr><td><a href="#/runs/${esc(run.run_id)}">${esc(run.session_title || run.run_id)}</a></td><td>${esc(run.host_name)}</td><td>${esc(agentMeta(run.agent).label)}</td><td>${esc(fmtStamp(run.started_at))}</td><td>${tokens(run.input_tokens)}</td><td>${tokens(run.output_tokens)}</td><td>${run.cost_usd == null ? '—' : `$${Number(run.cost_usd).toFixed(4)}`}</td><td>${esc(t(run.source || 'unavailable'))}</td></tr>`).join('')}
      </tbody></table></div>` : `<p class="muted">${t('No usage data for this period.')}</p>`}
    </section>`;

  const applyFilters = () => {
    const next = new URLSearchParams();
    next.set('period', document.getElementById('usage-period').value);
    const host = document.getElementById('usage-host').value;
    const agent = document.getElementById('usage-agent').value;
    if (host) next.set('host', host);
    if (agent) next.set('agent', agent);
    location.hash = '#/usage?' + next.toString();
  };
  for (const id of ['usage-period', 'usage-host', 'usage-agent']) document.getElementById(id).addEventListener('change', applyFilters);
  let timer = null;
  state.wsHandlers.add((msg) => {
    if (msg.type !== 'run.usage' && msg.type !== 'run.exit') return;
    clearTimeout(timer);
    timer = setTimeout(() => {
      if (pageGen === state.pageGen) pageUsage().catch(() => {});
    }, 1500);
  });
  state.pageCleanup = () => clearTimeout(timer);
}
