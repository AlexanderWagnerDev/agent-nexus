import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { agentGlyph, badge, toast } from '../ui.js';
import { shareHostDialog } from '../components/host-share.js';
import { newSessionDialog } from '../components/new-session.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { agentVersion, esc, fmtRelative, fmtTime } from '../ui-format.js';

function reasonIcon(severity) {
  return severity === 'error' ? svgIcon('alert', 14) : svgIcon('info', 14);
}

function parseDetails(raw) {
  try {
    const value = JSON.parse(raw || '{}');
    return value && typeof value === 'object' ? value : {};
  } catch {
    return {};
  }
}

export async function pageHostDetail(id) {
  const pageGen = state.pageGen;
  renderShell('hosts');
  const view = setPage(t('Host'), '', '');
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  let data;
  try {
    data = await api(`/hosts/${encodeURIComponent(id)}`);
  } catch (err) {
    if (pageGen !== state.pageGen) return;
    view.innerHTML = `<div class="card empty"><strong>${t('Host not found')}</strong>${esc(err.message)} <a href="#/hosts">${t('Back to hosts')}</a></div>`;
    return;
  }
  if (pageGen !== state.pageGen) return;
  const h = data.host;
  const health = h.health || { state: h.status, reasons: [] };
  const policy = h.policy || {};
  const update = h.update;
  const noRelease = update?.error === 'no published release';
  setPage(h.name, `${h.os}/${h.arch} · nexusd ${h.version || 'unknown'} · ${h.id}`, `
    <a class="btn small ghost" href="#/hosts">${t('Back to hosts')}</a>
    <button class="btn small ghost" id="host-favorite" type="button">${svgIcon('star')} ${h.favorite ? t('Unpin host') : t('Pin host')}</button>
    <button class="btn small" id="host-new-session" ${h.status !== 'online' || h.disabled ? 'disabled' : ''}>${t('New session')}</button>
    <button class="btn small" id="host-share">${t('Share')}</button>
    <button class="btn small" id="host-update-check">${t('Check for updates')}</button>`);

  const reasons = (health.reasons || []).map((reason) => `
    <div class="health-reason ${esc(reason.severity)}">
      ${reasonIcon(reason.severity)}
      <div><strong>${esc(reason.code.replaceAll('_', ' '))}</strong><span>${esc(reason.message)}</span></div>
    </div>`).join('');

  const agents = (h.agents || []).map((a) => `
    <tr>
      <td><span class="agent-inline">${agentGlyph(a.name, 'small')}<b>${esc(a.name)}</b></span></td>
      <td class="mono-small">${esc(agentVersion(a.name, a.version))}</td>
      <td>${a.supported ? badge(t('supported'), 'online') : badge(t('unsupported'), 'failed')}${a.untested ? badge(t('untested'), 'waiting_for_input') : ''}</td>
      <td class="wrap">${(a.capabilities || []).map((cap) => `<span class="chip tiny">${esc(cap)}</span>`).join(' ') || `<span class="muted">${t('not reported')}</span>`}</td>
      <td class="mono-small">${esc(a.path || '')}</td>
    </tr>
    ${a.version_warning ? `<tr class="agent-warning-row"><td colspan="5">${esc(a.version_warning)}</td></tr>` : ''}`).join('');

  const runs = (data.runs || []).map((run) => `
    <a class="event-row" href="#/runs/${encodeURIComponent(run.id)}">
      <span class="event-dot ${run.state === 'running' ? 'live' : ''}"></span>
      <span class="event-main"><b>${esc(run.agent || 'agent')}</b> ${esc(run.session_title || run.cwd || '')}</span>
      <span class="badge ${esc(run.state)}">${esc(t(run.state))}</span>
      <time>${esc(fmtRelative(run.started_at))}</time>
    </a>`).join('');

  const sessions = (data.recent_sessions || []).map((s) => `
    <a class="event-row" href="#/sessions/${encodeURIComponent(s.id)}">
      <span class="event-dot"></span>
      <span class="event-main"><b>${esc(s.title || s.native_id)}</b><small>${esc(s.cwd)}</small></span>
      <span class="event-meta">${esc(s.agent)} · ${t('{n} msgs', { n: s.message_count })}</span>
      <time>${esc(fmtRelative(s.updated_at))}</time>
    </a>`).join('');

  const events = (data.events || []).map((event) => {
    const details = parseDetails(event.details);
    return `
      <div class="event-row static">
        <span class="event-dot"></span>
        <span class="event-main"><b>${esc(event.action)}</b><small>${esc(event.actor)}${details.run_id ? ` · ${esc(details.run_id)}` : ''}</small></span>
        <time>${esc(fmtRelative(event.at))}</time>
      </div>`;
  }).join('');

  view.innerHTML = `
    <div class="detail-layout">
      <section class="detail-main">
        <div class="section-card">
          <div class="section-head"><h2>${t('Health')}</h2><span class="aside-note">${esc(fmtTime(health.checked_at))}</span></div>
          <div class="health-list">${reasons || `<div class="health-reason info">${svgIcon('check', 14)}<div><strong>${t('No problems detected')}</strong></div></div>`}</div>
        </div>

        <div class="section-card">
          <div class="section-head"><h2>${t('Coding agents')}</h2><span class="toolbar-count">${(h.agents || []).length}</span></div>
          ${agents ? `<div class="table-wrap"><table class="data-table"><thead><tr><th>${t('Agent')}</th><th>${t('Version')}</th><th>${t('Status')}</th><th>${t('Capabilities')}</th><th>${t('Path')}</th></tr></thead><tbody>${agents}</tbody></table></div>` : `<div class="empty compact">${t('No coding agent detected on this host.')}</div>`}
        </div>

        <div class="section-card">
          <div class="section-head"><h2>${t('Active runs')}</h2><span class="toolbar-count">${data.active_runs || 0}</span></div>
          ${runs || `<div class="empty compact">${t('No active runs on this host.')}</div>`}
        </div>

        <div class="section-card">
          <div class="section-head"><h2>${t('Recent sessions')}</h2><a class="aside-note" href="#/sessions?host=${encodeURIComponent(h.id)}">${t('All sessions')}</a></div>
          ${sessions || `<div class="empty compact">${t('No sessions discovered on this host yet.')}</div>`}
        </div>

        ${data.can_manage ? `<div class="section-card">
          <div class="section-head"><h2>${t('Host events')}</h2></div>
          ${events || `<div class="empty compact">${t('No audited host events yet.')}</div>`}
        </div>` : ''}
      </section>

      <aside class="detail-aside">
        <div class="section-card">
          <div class="section-head"><h2>${t('Connection')}</h2></div>
          <div class="meta-list">
            <div><span>${t('Status')}</span><strong>${esc(t(h.status))}</strong></div>
            <div><span>${t('Last contact')}</span><strong>${esc(fmtTime(h.last_seen))}</strong></div>
            <div><span>${t('Daemon')}</span><strong>${esc(h.version || 'unknown')}</strong></div>
            <div><span>${t('Platform')}</span><strong>${esc(h.os)}/${esc(h.arch)}</strong></div>
            <div><span>${t('Host ID')}</span><strong class="mono-small">${esc(h.id)}</strong></div>
          </div>
        </div>

        <div class="section-card">
          <div class="section-head"><h2>${t('Workspace')}</h2></div>
          <div class="meta-list">
            <div><span>${t('Sessions')}</span><strong>${data.sessions}</strong></div>
            <div><span>${t('Projects')}</span><strong>${data.projects}</strong></div>
            <div><span>${t('Active runs')}</span><strong>${data.active_runs}</strong></div>
            <div><span>${t('Index')}</span><strong>${h.index ? esc(t(h.index.state)) : t('Index off')}${h.index?.indexed ? ` · ${h.index.indexed}/${h.index.total}` : ''}</strong></div>
          </div>
        </div>

        <div class="section-card">
          <div class="section-head"><h2>${t('Policy')}</h2></div>
          <div class="meta-list">
            <div><span>${t('Allowed roots')}</span><strong class="wrap">${esc((policy.allowed_roots || []).join(', ') || t('not reported'))}</strong></div>
            <div><span>${t('Allowed agents')}</span><strong>${esc((policy.allowed_agents || []).join(', ') || t('all detected'))}</strong></div>
            <div><span>${t('New / fork')}</span><strong>${policy.allow_new_sessions === false ? t('new blocked') : t('new allowed')} · ${policy.allow_fork === false ? t('fork blocked') : t('fork allowed')}</strong></div>
          </div>
        </div>

        <div class="section-card">
          <div class="section-head"><h2>${t('Updates')}</h2></div>
          <div id="host-update-result">
            ${update ? `<div class="meta-list">
              <div><span>${t('Installed')}</span><strong>${esc(update.current || h.version || t('unknown'))}</strong></div>
              <div><span>${t('Latest')}</span><strong>${esc(update.latest || t('unknown'))}</strong></div>
              <div><span>${t('Status')}</span><strong>${noRelease ? t('No published release') : (update.error ? t('check failed') : (update.available ? t('update available') : t('up to date')))}</strong></div>
            </div>
            ${update.error && !noRelease ? `<div class="notice error small mt-1">${esc(update.error)}</div>` : ''}
            ${noRelease ? `<p class="aside-note mt-1">${t('The alpha has no release feed. Update checks resume after the first approved version.')}</p>` : ''}
            ${update.available ? `<div class="notice small mt-1">${t('Run on the host:')} <code>nexusd update --install</code> ${t('(refuses while runs are active)')}</div>` : ''}` : `<p class="aside-note">${t('Check the GitHub release feed. Installation stays on the host:')} <code>nexusd update --install</code>.</p>`}
          </div>
        </div>
      </aside>
    </div>`;

  document.getElementById('host-new-session')?.addEventListener('click', () => newSessionDialog(h));
  document.getElementById('host-favorite')?.addEventListener('click', async () => {
    try {
      await api(`/hosts/${encodeURIComponent(h.id)}/favorite`, { method: h.favorite ? 'DELETE' : 'PUT' });
      toast(h.favorite ? t('Host unpinned') : t('Host pinned'));
      pageHostDetail(h.id);
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('host-share')?.addEventListener('click', () => shareHostDialog(h));
  document.getElementById('host-update-check')?.addEventListener('click', async (event) => {
    const box = document.getElementById('host-update-result');
    const button = event.currentTarget;
    if (!box || button.disabled) return;
    button.disabled = true;
    const previous = box.innerHTML;
    box.innerHTML = `<div class="empty compact"><span class="spinner"></span>${t('Checking...')}</div>`;
    try {
      const info = await api(`/hosts/${encodeURIComponent(h.id)}/update`);
      box.innerHTML = `<div class="meta-list">
        <div><span>${t('Installed')}</span><strong>${esc(info.current || h.version || t('unknown'))}</strong></div>
        <div><span>${t('Latest')}</span><strong>${esc(info.latest || t('unknown'))}</strong></div>
        <div><span>${t('Status')}</span><strong>${info.available ? t('update available') : t('up to date')}</strong></div>
      </div>${info.available ? `<div class="notice small mt-1">${t('Run on the host:')} <code>nexusd update --install</code> ${t('(refuses while runs are active)')}</div>` : ''}`;
    } catch (err) {
      box.innerHTML = `<div class="notice error small">${esc(err.message)}</div>`;
      toast(err.message, true);
      setTimeout(() => { if (box) box.innerHTML = previous; }, 5000);
    } finally {
      button.disabled = false;
    }
  });

  for (const fn of state.wsHandlers) {
    if (fn.hostDetailID === h.id) state.wsHandlers.delete(fn);
  }
  const handler = (msg) => {
    if (msg.type === 'host.updated' && (!msg.host || msg.host.id === h.id)) pageHostDetail(h.id).catch(() => {});
    if ((msg.type === 'run.state' || msg.type === 'run.exit') && msg.run?.host_id === h.id) {
      pageHostDetail(h.id).catch(() => {});
    }
  };
  handler.hostDetailID = h.id;
  state.wsHandlers.add(handler);
}
