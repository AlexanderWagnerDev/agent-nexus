import { api } from '../api.js';
import { ensureStepUp } from '../auth.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { badge, confirmDanger, copyText, modal, toast } from '../ui.js';
import { shareHostDialog } from '../components/host-share.js';
import { newSessionDialog } from '../components/new-session.js';
import { t } from '../i18n.js';
import { agentVersion, esc, fmtRelative, fmtTime } from '../ui-format.js';

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
  renderShell('hosts');
  const view = setPage(t('Hosts'), t('Machines running nexusd. Pairing is always initiated from this hub.'), `<button class="btn primary small" id="pair-host">${t('Pair a host')}</button>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const [{ hosts }] = await Promise.all([api('/hosts')]);
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
        <div class="host-name"><span class="side-dot ${h.status === 'online' ? 'on' : ''}"></span>${esc(h.name)}${badge(t(h.pending ? 'pending' : (h.status === 'online' ? 'online' : 'offline')), h.pending ? 'waiting_for_input' : h.status)}${h.update?.available ? `<span class="badge waiting_for_input" title="${t('nexusd update available')}">${t('update')} ${esc(h.update.latest || '')}</span>` : ''}</div>
        <div class="sub">${esc(h.os)}/${esc(h.arch)} · nexusd ${esc(h.version || 'unknown')} · ${t('seen')} ${esc(fmtRelative(h.last_seen))}</div>
      </div>
      <div class="agent-chips">${(h.agents || []).map((a) => `<span class="chip"><b>${esc(a.name)}</b> ${esc(agentVersion(a.name, a.version))}</span>`).join('') || `<span class="chip">${t('no agents')}</span>`}<span class="chip ${h.index?.state === 'building' ? 'active' : ''}">${indexLabel(h.index)}</span></div>
      <div class="host-actions">
        <button class="btn small ghost" data-host-details="${esc(h.id)}">${t('Details')}</button>
        <button class="btn small" data-new-session="${esc(h.id)}" ${h.status !== 'online' ? 'disabled' : ''}>${t('New session')}</button>
        <button class="btn small" data-share-host="${esc(h.id)}">${t('Share')}</button>
        <button class="btn small danger" data-revoke-host="${esc(h.id)}">${t('Revoke')}</button>
      </div>
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
  view.querySelectorAll('[data-host-details]').forEach((b) => b.addEventListener('click', () => {
    const h = hosts.find((host) => host.id === b.dataset.hostDetails);
    if (!h) return;
    const policy = h.policy || {};
    modal(`
      <div class="modal-title-row"><h2>${esc(h.name)}</h2>${badge(t(h.pending ? 'pending' : h.status), h.pending ? 'waiting_for_input' : h.status)}</div>
      <p class="hint">${t('Host identity and the policy enforced locally by nexusd. The hub cannot override these limits.')}</p>
      <div class="meta-list">
        <div><span>${t('Host ID')}</span><strong class="mono-small">${esc(h.id)}</strong></div>
        <div><span>${t('Platform')}</span><strong>${esc(h.os)}/${esc(h.arch)}</strong></div>
        <div><span>${t('Daemon')}</span><strong>${esc(h.version || 'unknown')}</strong></div>
        <div><span>${t('Last seen')}</span><strong>${esc(fmtTime(h.last_seen))}</strong></div>
        <div><span>${t('Allowed roots')}</span><strong>${esc((policy.allowed_roots || []).join(', ') || t('not reported'))}</strong></div>
        <div><span>${t('Allowed agents')}</span><strong>${esc((policy.allowed_agents || []).join(', ') || t('all detected'))}</strong></div>
        <div><span>${t('New / fork')}</span><strong>${policy.allow_new_sessions === false ? t('new blocked') : t('new allowed')} · ${policy.allow_fork === false ? t('fork blocked') : t('fork allowed')}</strong></div>
      </div>
      <div class="aside-label mt-2">${t('Detected agents')}</div>
      <div class="agent-chips">${(h.agents || []).map((a) => `<span class="chip"><b>${esc(a.name)}</b> ${esc(agentVersion(a.name, a.version))}</span>`).join('') || `<span class="chip">${t('none')}</span>`}</div>
      <div class="aside-label mt-2">${t('Updates')}</div>
      <div id="host-update-result" class="host-update-result">${h.update ? `
        ${(() => {
          const noRelease = h.update.error === 'no published release';
          return `<div class="meta-list">
          <div><span>${t('Installed')}</span><strong>${esc(h.update.current || h.version || t('unknown'))}</strong></div>
          <div><span>${t('Latest')}</span><strong>${esc(h.update.latest || t('unknown'))}</strong></div>
          <div><span>${t('Status')}</span><strong>${noRelease ? t('No published release') : (h.update.error ? t('check failed') : (h.update.available ? t('update available') : t('up to date')))}</strong></div>
        </div>
        ${h.update.error && !noRelease ? `<div class="notice error small mt-1">${esc(h.update.error)}</div>` : ''}
        ${noRelease ? `<p class="aside-note mt-1">${t('The alpha has no release feed. Update checks resume after the first approved version.')}</p>` : ''}
        ${h.update.available ? `<div class="notice small mt-1">${t('Run on the host:')} <code>nexusd update --install</code> ${t('(refuses while runs are active)')}</div>` : ''}`;
        })()}
      ` : `<p class="aside-note">${t('Check the GitHub release feed. Installation stays on the host:')} <code>nexusd update --install</code>.</p>`}</div>
      <div class="modal-actions"><button class="btn small" id="host-update-check" type="button">${t('Check for updates')}</button><button class="btn" type="button" data-close>${t('Close')}</button></div>`);
    document.getElementById('host-update-check')?.addEventListener('click', async () => {
      const box = document.getElementById('host-update-result');
      const button = document.getElementById('host-update-check');
      if (!box || !button) return;
      button.disabled = true;
      box.innerHTML = `<div class="empty compact"><span class="spinner"></span>${t('Checking...')}</div>`;
      try {
        const info = await api(`/hosts/${encodeURIComponent(h.id)}/update`);
        box.innerHTML = `
          <div class="meta-list">
            <div><span>${t('Installed')}</span><strong>${esc(info.current || h.version || t('unknown'))}</strong></div>
            <div><span>${t('Latest')}</span><strong>${esc(info.latest || t('unknown'))}</strong></div>
            <div><span>${t('Status')}</span><strong>${info.available ? t('update available') : t('up to date')}</strong></div>
          </div>
          ${info.available ? `<div class="notice small mt-1">Run on the host: <code>nexusd update --install</code> (refuses while runs are active).</div>` : ''}`;
      } catch (err) {
        box.innerHTML = `<div class="notice error small">${esc(err.message)}</div><p class="aside-note mt-1">${t('Private releases require')} <code>NEXUS_GITHUB_TOKEN</code> ${t('with Contents: read on the host.')}</p>`;
      } finally {
        button.disabled = false;
      }
    });
  }));
  view.querySelectorAll('[data-share-host]').forEach((b) => b.addEventListener('click', () => shareHostDialog(hosts.find((h) => h.id === b.dataset.shareHost))));
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
