import { api } from '../api.js';
import { ensureStepUp } from '../auth.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { badge, closeModal, confirmDanger, copyText, modal, toast } from '../ui.js';
import { shareHostDialog } from '../components/host-share.js';
import { newSessionDialog } from '../components/new-session.js';
import { installCommand } from '../install-command.js';
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

// One debounced refresh per page instance. Re-registering a new closure on
// every host.updated event would multiply handlers and renders exponentially.
let hostRefreshTimer = null;

export async function pageHosts() {
  const pageGen = state.pageGen;
  renderShell('hosts');
  const view = setPage(t('Hosts'), t('Machines running nexusd. Pairing is always initiated from this hub.'), `<button class="btn small" id="update-all-hosts" type="button" hidden>${t('Update all')}</button><button class="btn primary small" id="pair-host">${t('Pair a host')}</button>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const [{ hosts }] = await Promise.all([api('/hosts')]);
  if (pageGen !== state.pageGen) return;
  hosts.sort((a, b) => Number(Boolean(b.favorite)) - Number(Boolean(a.favorite)) || a.name.localeCompare(b.name));
  document.getElementById('pair-host').addEventListener('click', pairHostDialog);
  const updateAll = document.getElementById('update-all-hosts');
  if (updateAll) {
    updateAll.hidden = !hosts.some((h) => h.status === 'online' && !h.disabled && !h.pending);
    updateAll.addEventListener('click', () => updateAllHosts(hosts, updateAll));
  }
  for (const handler of state.wsHandlers) {
    if (handler.hostsPage) state.wsHandlers.delete(handler);
  }
  const onHostUpdated = (msg) => {
    if (msg.type !== 'host.updated') return;
    clearTimeout(hostRefreshTimer);
    hostRefreshTimer = setTimeout(() => {
      if (pageGen === state.pageGen) pageHosts().catch(() => {});
    }, 200);
  };
  onHostUpdated.hostsPage = true;
  state.wsHandlers.add(onHostUpdated);
  state.pageCleanup = () => {
    clearTimeout(hostRefreshTimer);
    hostRefreshTimer = null;
  };
  if (!hosts.length) {
    view.innerHTML = `<div class="card empty"><strong>${t('No hosts paired')}</strong>${t('Create a one-time code, then run')} <code>nexusd pair --hub ${esc(location.origin)} --code &lt;code&gt;</code> ${t('on the machine.')}</div>`;
    return;
  }
  view.innerHTML = `<div class="list host-list">${hosts.map((h) => `
    <div class="host-row">
      <div class="host-id">
        <div class="host-name"><span class="side-dot ${h.status === 'online' ? 'on' : ''}"></span><a href="#/hosts/${encodeURIComponent(h.id)}" class="host-name-link">${esc(h.name)}</a>${badge(t(h.pending ? 'pending' : (h.disabled ? 'disabled' : (h.status === 'online' ? 'online' : 'offline'))), h.pending ? 'waiting_for_input' : (h.disabled ? 'failed' : h.status))}${h.health ? badge(t(h.health.state), h.health.state === 'healthy' ? 'online' : (h.health.state === 'degraded' ? 'waiting_for_input' : 'offline')) : ''}${h.update?.available ? `<span class="badge waiting_for_input" title="${t('nexusd update available')}">${t('update')} ${esc(h.update.latest || '')}</span>` : ''}${['failed', 'rolled_back', 'blocked'].includes(h.update?.state) ? badge(t(h.update.state), 'failed') : ''}</div>
        <div class="sub">${esc(h.os)}/${esc(h.arch)} · nexusd ${esc(h.version || 'unknown')} · ${t('seen')} ${esc(fmtRelative(h.last_seen))}</div>
      </div>
      <div class="agent-chips">${(h.agents || []).map((a) => `<span class="chip ${a.untested ? 'warn' : ''}" ${a.version_warning ? `title="${esc(a.version_warning)}"` : ''}><b>${esc(a.name)}</b> ${esc(agentVersion(a.name, a.version))}${a.untested ? ' ?' : ''}</span>`).join('') || `<span class="chip">${t('no agents')}</span>`}<span class="chip ${h.index?.state === 'building' ? 'active' : ''}">${indexLabel(h.index)}</span></div>
      <div class="host-actions">
        <button class="icon-btn ${h.favorite ? 'active' : ''}" data-fav-host="${esc(h.id)}" type="button" title="${h.favorite ? t('Unpin host') : t('Pin host')}" aria-label="${h.favorite ? t('Unpin host') : t('Pin host')}">${svgIcon('star', 14)}</button>
        <a class="btn small ghost" href="#/hosts/${encodeURIComponent(h.id)}">${t('Details')}</a>
        <button class="btn small" data-new-session="${esc(h.id)}" ${h.status !== 'online' || h.disabled ? 'disabled' : ''}>${t('New session')}</button>
        <details class="action-menu">
          <summary class="icon-btn" aria-label="${t('More host actions')}" title="${t('More host actions')}">${svgIcon('menu', 16)}</summary>
          <div class="action-menu-panel">
            ${h.status === 'online' && !h.disabled ? `<button type="button" data-check-update="${esc(h.id)}">${svgIcon('refresh', 14)}${t('Check for updates')}</button>` : ''}
            ${h.status === 'online' && !h.disabled && h.update?.available ? `<button type="button" data-install-update="${esc(h.id)}">${svgIcon('arrowDown', 14)}${t('Install update')}</button>` : ''}
            <button type="button" data-rename-host="${esc(h.id)}">${svgIcon('edit', 14)}${t('Rename host')}</button>
            <button type="button" data-toggle-host="${esc(h.id)}" class="${h.disabled ? '' : 'danger'}">${svgIcon(h.disabled ? 'check' : 'stop', 14)}${h.disabled ? t('Enable host') : t('Disable host')}</button>
            <button type="button" data-share-host="${esc(h.id)}">${svgIcon('external', 14)}${t('Share')}</button>
            <button type="button" class="danger" data-revoke-host="${esc(h.id)}">${svgIcon('x', 14)}${t('Revoke')}</button>
          </div>
        </details>
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
  view.querySelectorAll('[data-check-update]').forEach((b) => b.addEventListener('click', async () => {
    const h = hosts.find((host) => host.id === b.dataset.checkUpdate);
    if (!h || b.disabled) return;
    b.disabled = true;
    try {
      const info = await api(`/hosts/${encodeURIComponent(h.id)}/update`);
      toast(info.available
        ? t('Update available on {name}: {version}', { name: h.name, version: info.latest })
        : t('{name} is up to date.', { name: h.name }));
      pageHosts();
    } catch (err) {
      toast(err.message, true);
      b.disabled = false;
    }
  }));
  view.querySelectorAll('[data-install-update]').forEach((b) => b.addEventListener('click', () => {
    const h = hosts.find((host) => host.id === b.dataset.installUpdate);
    if (h) installHostUpdate(h);
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
    confirmDanger(t('Revoking this host closes its connection and fails active runs. The daemon must be paired again.'), async () => {
      await api(`/hosts/${b.dataset.revokeHost}`, { method: 'DELETE' });
      toast(t('Host revoked'));
      pageHosts();
    });
  }));
}

async function installHostUpdate(host) {
  if (host.update?.remote_install) {
    confirmDanger(t('Install the available nexusd update on {name}? The host waits for active runs to finish and verifies the release signature.', { name: host.name }), async () => {
      try {
        await ensureStepUp();
        await api(`/hosts/${encodeURIComponent(host.id)}/update/install`, { method: 'POST' });
        toast(t('Installation requested'));
        pageHosts();
      } catch (err) {
        toast(err.message, true);
      }
    });
    return;
  }
  confirmDanger(t('Remote installation is disabled on {name}. Enable it for this host and install the update? The host still verifies the release signature and waits for active runs.', { name: host.name }), async () => {
    try {
      await ensureStepUp();
      await api(`/hosts/${encodeURIComponent(host.id)}/update/config`, { method: 'POST', body: { allow_remote_install: true } });
      await api(`/hosts/${encodeURIComponent(host.id)}/update/install`, { method: 'POST' });
      toast(t('Remote installation enabled; installation requested'));
      pageHosts();
    } catch (err) {
      toast(err.message, true);
    }
  });
}

async function updateAllHosts(hosts, button) {
  const online = hosts.filter((h) => h.status === 'online' && !h.disabled && !h.pending);
  if (!online.length) {
    toast(t('No online hosts to update.'));
    return;
  }
  button.disabled = true;
  button.innerHTML = `<span class="spinner"></span> ${t('Checking...')}`;
  try {
    await Promise.allSettled(online.map((h) => api(`/hosts/${encodeURIComponent(h.id)}/update`)));
    const { hosts: fresh } = await api('/hosts');
    const targets = fresh.filter((h) => h.status === 'online' && !h.disabled && !h.pending && h.update?.available);
    if (!targets.length) {
      toast(t('No updates available.'));
      pageHosts();
      return;
    }
    openUpdateAllDialog(targets);
  } catch (err) {
    toast(err.message, true);
  } finally {
    button.disabled = false;
    button.innerHTML = t('Update all');
  }
}

function openUpdateAllDialog(targets) {
  const ready = targets.filter((h) => h.update?.remote_install);
  const blocked = targets.filter((h) => !h.update?.remote_install);
  const rows = targets.map((h) => `<li><b>${esc(h.name)}</b>${h.update?.latest ? ` → ${esc(h.update.latest)}` : ''}${h.update?.remote_install ? '' : ` · ${t('remote installation off')}`}</li>`).join('');
  modal(`
    <h2>${t('Update hosts')}</h2>
    <p class="hint">${t('{n} hosts have an update available.', { n: targets.length })}</p>
    <ul class="small">${rows}</ul>
    ${blocked.length ? `<div class="notice small mt-1">${t('Remote installation is off on {n} hosts. Enabling it lets this hub request signed updates for those hosts.', { n: blocked.length })}</div>` : ''}
    <div class="modal-actions">
      <button class="btn ghost" type="button" data-close>${t('Cancel')}</button>
      ${ready.length ? `<button class="btn primary" type="button" id="update-all-ready">${t('Update {n} hosts', { n: ready.length })}</button>` : ''}
      ${blocked.length ? `<button class="btn ${ready.length ? '' : 'primary'}" type="button" id="update-all-enable">${t('Enable on {n} and update', { n: blocked.length })}</button>` : ''}
    </div>`);
  const run = async (selected, enableRemote) => {
    try {
      await ensureStepUp();
      let ok = 0;
      const failures = [];
      for (const h of selected) {
        try {
          if (enableRemote) {
            await api(`/hosts/${encodeURIComponent(h.id)}/update/config`, { method: 'POST', body: { allow_remote_install: true } });
          }
          await api(`/hosts/${encodeURIComponent(h.id)}/update/install`, { method: 'POST' });
          ok += 1;
        } catch (err) {
          failures.push(`${h.name}: ${err.message}`);
        }
      }
      closeModal();
      if (ok) toast(t('Update requested on {n} hosts', { n: ok }));
      if (failures.length) toast(failures.join(' · '), true);
      pageHosts();
    } catch (err) {
      toast(err.message, true);
    }
  };
  document.getElementById('update-all-ready')?.addEventListener('click', () => run(ready, false));
  document.getElementById('update-all-enable')?.addEventListener('click', () => run(blocked, true));
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
    const steps = document.getElementById('pair-steps');
    steps.innerHTML = `
      <p class="small muted mt-2">${t('Run this one-liner on the machine you want to add. It installs, pairs and starts the service.')}</p>
      <div class="split" id="install-os" role="group" aria-label="${t('Operating system')}">
        <button type="button" class="btn small" data-install-os="linux">Linux</button>
        <button type="button" class="btn small" data-install-os="macos">macOS</button>
        <button type="button" class="btn small" data-install-os="windows">Windows</button>
      </div>
      <div class="secret mt-1" id="install-command"></div>
      <div class="split mt-1">
        <button class="btn small" id="copy-install-command">${t('Copy install command')}</button>
        <button class="btn small" id="copy-code">${t('Copy code')}</button>
      </div>
      <details class="mt-2">
        <summary class="small muted">${t('Already installed? Pair manually:')}</summary>
        <div class="secret mt-1">nexusd pair --hub ${esc(location.origin)} --code ${esc(data.code)}</div>
      </details>`;
    let os = /Win/i.test(navigator.userAgent) ? 'windows' : (/Mac/i.test(navigator.userAgent) ? 'macos' : 'linux');
    const renderCommand = () => {
      document.getElementById('install-command').textContent = installCommand(os, location.origin, data.code, state.version);
      document.querySelectorAll('[data-install-os]').forEach((button) => {
        button.classList.toggle('primary', button.dataset.installOs === os);
      });
    };
    document.querySelectorAll('[data-install-os]').forEach((button) => button.addEventListener('click', () => {
      os = button.dataset.installOs;
      renderCommand();
    }));
    renderCommand();
    document.getElementById('copy-install-command').addEventListener('click', () => copyText(document.getElementById('install-command').textContent));
    document.getElementById('copy-code').addEventListener('click', () => copyText(data.code));
  }).catch((err) => {
    const box = document.getElementById('pair-code');
    if (box) { box.className = 'notice error'; box.textContent = err.message; }
  });
}
