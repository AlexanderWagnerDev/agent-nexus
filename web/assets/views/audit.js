import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { badge } from '../ui.js';
import { t } from '../i18n.js';
import { esc, fmtTime } from '../ui-format.js';

export async function pageAudit() {
  const pageGen = state.pageGen;
  renderShell('audit');
  const view = setPage(t('Audit log'), t('Append-only, hash-chained security events stored on the hub.'));
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const data = await api('/audit?limit=300');
  if (pageGen !== state.pageGen) return;
  view.innerHTML = `
    ${data.broken_seq ? `<div class="notice error mb-2">${t('Audit chain broken at sequence')} ${esc(data.broken_seq)}. ${t('Investigate the database immediately.')}</div>` : `<div class="notice info mb-2">${t('Hash chain verified: no broken link in the checked range.')}</div>`}
    <div class="list">${data.events.map((e) => `
      <div class="audit-row">
        <div class="split wrap"><span class="badge no-dot">#${esc(e.seq)}</span><strong>${esc(e.action)}</strong><span class="right small muted">${esc(fmtTime(e.at))} · ${esc(e.actor)}</span></div>
        ${e.details && e.details !== 'null' ? `<pre class="audit-details">${esc(e.details)}</pre>` : ''}
      </div>`).join('')}</div>`;
}
