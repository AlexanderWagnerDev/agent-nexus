import { api } from '../api.js';
import { ensureStepUp } from '../auth.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { badge, confirmDanger, copyText, modal, toast } from '../ui.js';
import { shareHostDialog } from '../components/host-share.js';
import { newSessionDialog } from '../components/new-session.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { agentVersion, esc, fmtRelative } from '../ui-format.js';

function indexLabel(index) {
  if (!index || index.state === 'disabled') return t('Index off');
  if (index.state === 'building') {
    const pct = index.total ? Math.min(100, Math.floor((index.indexed * 100) / index.total)) : 0;
    return t('Indexing: {n}%', { n: pct });
  }
  if (index.state === 'error') return t('Index error');
  return t('Index ready');
}

export async function pageHosts() {
  const pageGen = state.pageGen;
  renderShell('hosts');
  const view = setPage(t('Hosts'), t('Machines running nexusd. Pairing is always initiated from this hub.'), `<button class="btn primary small" id="pair-host">${t('Pair a host')}</button>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const [{ hosts }] = await Promise.all([api('/hosts')]);
  if (pageGen !== state.pageGen) return;
  hosts.sort((a, b) => Number(Boolean(b.favorite)) - Number(Boolean(a.favorite)) || a.name.localeCompare(b.name));
  document.getElementById('pair-host').addEventListener('click', pairHostDialog);
  state.wsHandlers.add((msg) => {
    if (msg.type === 'host.updated') pageHosts().catch(() => {});
    if (msg.type === 'sessions.changed') { /* no-op on hosts page */ }
  });
  if (!hosts.length) {
    view.innerHTML = `<div class="card empty"><strong>${t('No hosts paired')}</strong>${t('Create a one-time code, then run')} <code>nexusd pair --hub ${esc(location.origin)} --code &lt;code&gt;</code> ${t('on the machine.')}</div>`;
    return;
  }
  view.innerHTML = `<div class="list">${hosts.map((h) => `
    <div class="host-row">
      <div class="host-id">
        <div class="host-name"><span class="side-dot ${h.status === 'online' ? 'on' : ''}"></span><a href="#/hosts/${encodeURIComponent(h.id)}" class="host-name-link">${esc(h.name)}</a>${badge(t(h.pending ? 'pending' : (h.disabled ? 'disabled' : (h.status === 'online' ? 'online' : 'offline'))), h.pending ? 'waiting_for_input' : (h.disabled ? 'failed' : h.status))}${h.health ? badge(t(h.health.state), h.health.state === 'healthy' ? 'online' : (h.health.state === 'degraded' ? 'waiting_for_input' : 'offline')) : ''}${h.update?.available ? `<span class="badge waiting_for_input" title="${t('nexusd update available')}">${t('update')} ${esc(h.update.latest || '')}</span>` : ''}</div>
        <div class="sub">${esc(h.os)}/${esc(h.arch)} · nexusd ${esc(h.version || 'unknown')} · ${t('seen')} ${esc(fmtRelative(h.last_seen))}</div>
      </div>
      <div class="agent-chips">${(h.agents || []).map((a) => `<span class="chip ${a.untested ? 'warn' : ''}" ${a.version_warning ? `title="${esc(a.version_warning)}"` : ''}><b>${esc(a.name)}</b> ${esc(agentVersion(a.name, a.version))}${a.untested ? ' ?' : ''}</span>`).join('') || `<span class="chip">${t('no agents')}</span>`}<span class="chip ${h.index?.state === 'building' ? 'active' : ''}">${indexLabel(h.index)}</span></div>
      <div class="host-actions">
        <button class="icon-btn ${h.favorite ? 'active' : ''}" data-fav-host="${esc(h.id)}" type="button" title="${h.favorite ? t('Unpin host') : t('Pin host')}" aria-label="${h.favorite ? t('Unpin host') : t('Pin host')}">${svgIcon('star', 14)}</button>
        <a class="btn small ghost" href="#/hosts/${encodeURIComponent(h.id)}">${t('Details')}</a>
        <button class="btn small" data-rename-host="${esc(h.id)}">${t('Rename host')}</button>
        <button class="btn small ${h.disabled ? '' : 'danger'}" data-toggle-host="${esc(h.id)}">${h.disabled ? t('Enable host') : t('Disable host')}</button>
        <button class="btn small" data-new-session="${esc(h.id)}" ${h.status !== 'online' || h.disabled ? 'disabled' : ''}>${t('New session')}</button>
        <button class="btn small" data-share-host="${esc(h.id)}">${t('Share')}</button>
        <button class="btn small danger" data-revoke-host="${esc(h.id)}">${t('Revoke')}</button>
      </div>
      ${h.disabled ? `<div class="notice info small mt-1">${t('A host must be enabled before it can run agents again.')}</div>` : ''}
      ${h.pending ? `<div class="host-pending"><span>${t('Compare SAS with the daemon.')}</span><span class="secret">${esc(h.sas)}</span><button class="btn primary small" data-confirm-host="${esc(h.id)}">${t('Confirm host')}</button></div>` : ''}
    </div>`).join('')}</div>`;
  view.querySelectorAll('[data-new-session]').forEach((b) => b.addEventListener('click', () => newSessionDialog(hosts.find((h) => h.id === b.dataset.newSession))));
  view.querySelectorAll('[data-confirm-host]').forEach((b) => b.addEventListener('click', async () => {
    try {
      await api(`/hosts/${b.dataset.confirmHost}/confirm`, { method: 'POST' });
      toast(t('Host confirmed'));
      pageHosts();
    } catch (err) {
      toast(err.message, true);
    }
  }));
  view.querySelectorAll('[data-rename-host]').forEach((b) => b.addEventListener('click', () => {
    const h = hosts.find((host) => host.id === b.dataset.renameHost);
    if (!h) return;
    modal(`
      <h2>${t('Rename host')}</h2>
      <label class="label">${t('Host name')}<input class="input" id="rename-host-name" maxlength="80" value="${esc(h.name)}"></label>
      <p class="hint">${t('The name is stored on the hub and is not reverted by host reconnects.')}</p>
      <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Cancel')}</button><button class="btn primary" type="button" id="rename-host-save">${t('Save')}</button></div>`);
    document.getElementById('rename-host-save').addEventListener('click', async () => {
      try {
        await ensureStepUp();
        await api(`/hosts/${encodeURIComponent(h.id)}`, { method: 'PATCH', body: { name: document.getElementById('rename-host-name').value.trim() } });
        toast(t('Host renamed'));
        pageHosts();
      } catch (err) { toast(err.message, true); }
    });
  }));
  view.querySelectorAll('[data-toggle-host]').forEach((b) => b.addEventListener('click', () => {
    const h = hosts.find((host) => host.id === b.dataset.toggleHost);
    if (!h) return;
    const apply = async () => {
      try {
        await ensureStepUp();
        await api(`/hosts/${encodeURIComponent(h.id)}`, { method: 'PATCH', body: { disabled: !h.disabled } });
        toast(h.disabled ? t('Host enabled') : t('Host disabled'));
        pageHosts();
      } catch (err) { toast(err.message, true); }
    };
    if (h.disabled) { apply(); return; }
    confirmDanger(t('Disabled hosts accept no new runs. Active runs are stopped when a host is disabled.'), apply);
  }));
  view.querySelectorAll('[data-share-host]').forEach((b) => b.addEventListener('click', () => shareHostDialog(hosts.find((h) => h.id === b.dataset.shareHost))));
  view.querySelectorAll('[data-fav-host]').forEach((b) => b.addEventListener('click', async () => {
    const host = hosts.find((h) => h.id === b.dataset.favHost);
    if (!host) return;
    try {
      await api(`/hosts/${encodeURIComponent(host.id)}/favorite`, { method: host.favorite ? 'DELETE' : 'PUT' });
      pageHosts();
    } catch (err) { toast(err.message, true); }
  }));
  view.querySelectorAll('[data-revoke-host]').forEach((b) => b.addEventListener('click', () => {
    confirmDanger('Revoking this host closes its connection and fails active runs. The daemon must be paired again.', async () => {
      await api(`/hosts/${b.dataset.revokeHost}`, { method: 'DELETE' });
      toast(t('Host revoked'));
      pageHosts();
    });
  }));
}

export async function pairHostDialog() {
  try {
    await ensureStepUp();
  } catch {
    return;
  }
  modal(`
    <h2>${t('Pair a host')}</h2>
    <p class="hint">${t('The code is single-use and expires after 10 minutes. The host connects outbound; no inbound ports are required.')}</p>
    <div class="empty" id="pair-code"><span class="spinner"></span></div>
    <div id="pair-steps"></div>
    <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Close')}</button></div>`);
  api('/hosts/pairing-codes', { method: 'POST' }).then((data) => {
    const box = document.getElementById('pair-code');
    if (!box) return;
    box.className = 'secret';
    box.textContent = data.code;
    const origin = location.origin.replace(/^http/, 'ws');
    document.getElementById('pair-steps').innerHTML = `
      <p class="small muted mt-2">${t('Run this on the machine you want to add:')}</p>
      <div class="secret">nexusd pair --hub ${esc(location.origin)} --code ${esc(data.code)}</div>
      <div class="split mt-1">
        <button class="btn small" id="copy-code">${t('Copy code')}</button>
        <button class="btn small" id="copy-command">${t('Copy command')}</button>
      </div>
      <p class="small muted mt-2">${t('Then install the per-user service:')} <code>nexusd service install</code>. ${t('The host is accepted by this hub at')} <code>${esc(origin)}/v1/host/connect</code>.</p>`;
    document.getElementById('copy-code').addEventListener('click', () => copyText(data.code));
    document.getElementById('copy-command').addEventListener('click', () => copyText(`nexusd pair --hub ${location.origin} --code ${data.code}`));
  }).catch((err) => {
    const box = document.getElementById('pair-code');
    if (box) { box.className = 'notice error'; box.textContent = err.message; }
  });
}
