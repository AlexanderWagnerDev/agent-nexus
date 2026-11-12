import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { badge, toast } from '../ui.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { esc, fmtRelative, fmtTime } from '../ui-format.js';

function seconds(value) {
  const n = Number(value || 0);
  if (n < 90) return `${n}s`;
  if (n < 5400) return `${Math.round(n / 60)}m`;
  if (n < 172800) return `${(n / 3600).toFixed(1)}h`;
  return `${(n / 86400).toFixed(1)}d`;
}

function healthBadge(host) {
  const cls = host.health === 'healthy' ? 'online' : host.health === 'offline' ? 'offline' : host.health === 'disabled' ? 'failed' : 'waiting_for_input';
  return badge(t(host.health), cls);
}

export async function pageDiagnostics() {
  const pageGen = state.pageGen;
  renderShell('settings');
  const view = setPage(t('Diagnostics'), t('Hub, database, host and queue status. No transcript content or secrets are included.'));
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  let data;
  try {
    data = await api('/admin/diagnostics');
  } catch (err) {
    if (pageGen !== state.pageGen) return;
    view.innerHTML = `<div class="card empty"><strong>${t('Diagnostics are available to administrators only')}</strong>${esc(err.message)}</div>`;
    return;
  }
  if (pageGen !== state.pageGen) return;
  const hub = data.hub || {};
  const counts = data.counts || {};
  const database = data.database || {};
  const hostRows = ((data.hosts || {}).items || []).map((host) => `
    <tr>
      <td><b>${esc(host.name || '')}</b><small class="mono-small">${esc(host.os)}/${esc(host.arch)} · ${esc(host.daemon || 'unknown')}</small></td>
      <td>${badge(t(host.status), host.status === 'online' ? 'online' : host.status === 'disabled' ? 'failed' : 'offline')}</td>
      <td>${healthBadge(host)}${(host.health_reasons || []).length ? `<small class="mono-small">${esc((host.health_reasons || []).join(', '))}</small>` : ''}</td>
      <td>${(host.agents || []).map((a) => `<span class="chip tiny">${esc(a.name)} ${esc(a.version)}</span>`).join(' ') || `<span class="muted">${t('none')}</span>`}</td>
      <td>${host.update?.available ? badge(t('update available'), 'waiting_for_input') : `<span class="muted">${t('up to date')}</span>`}</td>
    </tr>`).join('');
  const errors = (data.errors || []).map((entry) => `
    <div class="event-row static">
      <span class="event-dot"></span>
      <span class="event-main"><b>${esc(entry.action)}</b><small>${esc(entry.actor || '')}${entry.details ? ` · ${esc(entry.details)}` : ''}</small></span>
      <time>${esc(fmtRelative(entry.at))}</time>
    </div>`).join('');
  view.innerHTML = `
    <div class="toolbar">
      <span class="toolbar-count">${t('Generated')} ${esc(fmtTime(data.generated_at))}</span>
      <span class="grow"></span>
      <button class="btn small" id="diag-export" type="button">${svgIcon('file')} ${t('Export redacted diagnostics')}</button>
    </div>
    <div class="usage-cards">
      <div class="card usage-card"><span class="aside-label">${t('Hub version')}</span><strong>${esc(hub.version || 'unknown')}</strong><small class="mono-small">${esc(hub.os)}/${esc(hub.arch)} · Go ${esc(hub.go_version || '')}</small></div>
      <div class="card usage-card"><span class="aside-label">${t('Uptime')}</span><strong>${esc(seconds(hub.uptime_seconds))}</strong><small class="mono-small">${t('Since')} ${esc(fmtTime(hub.started_at))}</small></div>
      <div class="card usage-card"><span class="aside-label">${t('Database')}</span><strong>${database.ok ? t('OK') : t('Error')}</strong><small class="mono-small">${esc(database.error || '')}</small></div>
      <div class="card usage-card"><span class="aside-label">${t('Hosts')}</span><strong>${counts.hosts || 0}</strong><small class="mono-small">${(data.hosts || {}).online || 0} ${t('online')}</small></div>
      <div class="card usage-card"><span class="aside-label">${t('Sessions')}</span><strong>${counts.sessions || 0}</strong><small class="mono-small">${t('metadata only')}</small></div>
      <div class="card usage-card"><span class="aside-label">${t('Active runs')}</span><strong>${counts.active_runs || 0}</strong><small class="mono-small">${t('queued prompts')}: ${counts.queued_prompts || 0}</small></div>
      <div class="card usage-card"><span class="aside-label">${t('Tasks')}</span><strong>${counts.tasks || 0}</strong><small class="mono-small">${t('Users')}: ${counts.users || 0}</small></div>
      <div class="card usage-card"><span class="aside-label">${t('Configuration')}</span><strong>${data.config?.tls_enabled ? t('TLS') : t('HTTP')}</strong><small class="mono-small">${data.config?.metrics_enabled ? t('metrics on') : t('metrics off')} · ${data.config?.public_url_configured ? t('public URL set') : t('public URL unset')}</small></div>
    </div>
    <div class="section-card mt-2">
      <div class="section-head"><h2>${t('Hosts')}</h2><span class="toolbar-count">${(data.hosts || {}).total || 0}</span></div>
      ${hostRows ? `<div class="table-wrap"><table class="data-table"><thead><tr><th>${t('Host')}</th><th>${t('Status')}</th><th>${t('Health')}</th><th>${t('Agents')}</th><th>${t('Updates')}</th></tr></thead><tbody>${hostRows}</tbody></table></div>` : `<div class="empty compact">${t('No hosts paired')}</div>`}
    </div>
    ${(data.updates || []).length ? `<div class="section-card">
      <div class="section-head"><h2>${t('Updates')}</h2></div>
      ${(data.updates || []).map((u) => `<div class="event-row static"><span class="event-dot live"></span><span class="event-main"><b>${esc(u.host)}</b><small>${esc(u.current)} → ${esc(u.latest)}</small></span><time>${esc(fmtRelative(u.checked_at))}</time></div>`).join('')}
    </div>` : ''}
    <div class="section-card">
      <div class="section-head"><h2>${t('Recent errors')}</h2></div>
      ${errors || `<div class="empty compact">${t('No recent failures recorded.')}</div>`}
    </div>`;
  document.getElementById('diag-export').addEventListener('click', async () => {
    try {
      const exported = await api('/admin/diagnostics/export');
      const blob = new Blob([JSON.stringify(exported, null, 2)], { type: 'application/json' });
      const link = document.createElement('a');
      link.href = URL.createObjectURL(blob);
      link.download = 'agent-nexus-diagnostics.json';
      link.click();
      setTimeout(() => URL.revokeObjectURL(link.href), 1000);
      toast(t('Diagnostics exported'));
    } catch (err) {
      toast(err.message, true);
    }
  });
}
