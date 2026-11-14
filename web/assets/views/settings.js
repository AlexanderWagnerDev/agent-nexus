import { enablePush, logout } from '../auth.js';
import { api } from '../api.js';
import { renderShell, saveTheme, setPage } from '../shell.js';
import { state } from '../state.js';
import { badge, closeModal, copyText, modal, toast } from '../ui.js';
import { getLang, setLang, t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { esc, fmtStamp } from '../ui-format.js';

export async function pageSettings() {
  const pageGen = state.pageGen;
  renderShell('settings');
  const [hostData, settings, channelData, tokenData] = await Promise.all([
    api('/hosts').catch(() => ({ hosts: [] })),
    api('/me/settings').catch(() => null),
    api('/me/notification-channels').catch(() => ({ channels: [] })),
    api('/me/api-tokens').catch(() => ({ tokens: [] })),
  ]);
  if (pageGen !== state.pageGen) return;
  const hosts = hostData.hosts || [];
  const hostPrefs = settings?.notifications_by_host || {};
  const channels = channelData?.channels || [];
  const tokens = tokenData?.tokens || [];
  if (settings) state.settings = { ...state.settings, ...settings };
  const view = setPage(t('Settings'), t('Appearance, account, notifications and administration.'));
  const isAdmin = state.me.role === 'admin' || state.me.role === 'owner';
  const notifyKinds = [['approval', t('Approval required')], ['completed', t('Run completed')], ['failed', t('Run failed')], ['disconnected', t('Host disconnected')]];
  view.innerHTML = `
    <div class="grid cols-2">
      <section class="card">
        <h2 class="mt-0">${t('Appearance')}</h2>
        <p class="hint">${t('The theme is stored on the hub for your user and follows you across devices.')}</p>
        <div class="theme-choices" role="group" aria-label="${t('Theme')}">
          ${[['system', 'monitor', 'System', 'Follow the operating system'], ['dark', 'moon', 'Dark', 'Always dark'], ['light', 'sun', 'Light', 'Always light']].map(([value, icon, label, hint]) => `
            <button type="button" class="theme-choice ${state.settings.theme === value ? 'active' : ''}" data-theme-choice="${value}" aria-pressed="${state.settings.theme === value}">
              <span class="theme-icon" aria-hidden="true">${svgIcon(icon, 16)}</span>
              <span><strong>${t(label)}</strong><small>${t(hint)}</small></span>
            </button>`).join('')}
        </div>
        <label class="label mt-2">${t('Language')}<select class="select" id="settings-language">
          <option value="en" ${getLang() === 'en' ? 'selected' : ''}>English</option>
          <option value="de" ${getLang() === 'de' ? 'selected' : ''}>Deutsch</option>
        </select></label>
        <p class="hint mt-1">${t('The interface language is stored on this device. English is the default; changing it reloads the page.')}</p>
      </section>
      <section class="card">
        <h2 class="mt-0">${t('Account')}</h2>
        <div class="list">
          <div class="list-row"><div><div class="title">${esc(state.me.username)}</div><div class="sub"><span>${esc(t(state.me.role))}</span><span>${state.me.totp_enabled ? t('TOTP enabled') : t('TOTP not enabled')}</span></div></div></div>
        </div>
        <div class="split wrap mt-2">
          <a class="btn small" href="#/devices">${t('Devices & passkeys')}</a>
          <button class="btn small ghost" id="settings-signout">${t('Sign out')}</button>
        </div>
      </section>
      <section class="card">
        <h2 class="mt-0">${t('Notifications')}</h2>
        <p class="hint">${t('Content-free Web Push for approvals, completed runs, failures and host disconnects. The in-app inbox always keeps every event.')}</p>
        <div id="settings-push-area"><button class="btn" id="settings-push">${t('Enable on this device')}</button></div>
        <div class="setting-list mt-2">
          ${notifyKinds.map(([kind, label]) => `
            <label class="setting-row"><span>${label}</span><input type="checkbox" data-notify-kind="${kind}" ${state.settings.notifications?.[kind] !== false ? 'checked' : ''}></label>`).join('')}
        </div>
        <p class="hint mt-1">${t('Turning a category off only stops push delivery. Events still appear in the notification inbox.')}</p>
        ${hosts.length ? `
        <div class="aside-label mt-2">${t('Per-host overrides')}</div>
        <p class="hint mt-1">${t('Host-specific settings replace the global default for that host.')}</p>
        <label class="label">${t('Host')}<select class="select" id="settings-notify-host">${hosts.map((h) => `<option value="${esc(h.id)}">${esc(h.name)}</option>`).join('')}</select></label>
        <div class="setting-list" id="settings-host-notify">
          ${notifyKinds.map(([kind, label]) => `
            <label class="setting-row"><span>${label}</span><input type="checkbox" data-host-notify-kind="${kind}"></label>`).join('')}
        </div>
        <div class="split wrap mt-2">
          <button class="btn small ghost" id="settings-host-notify-reset" type="button">${t('Use global defaults')}</button>
          <span class="small muted" id="settings-host-notify-state"></span>
        </div>` : ''}
      </section>
      <section class="card">
        <h2 class="mt-0">${t('Notification channels')}</h2>
        <p class="hint">${t('Optional ntfy, Gotify, Telegram, Discord, Slack or generic webhook delivery. Channel payloads contain no chat content and use the same event preferences as Web Push.')}</p>
        <div class="setting-list" id="settings-channel-list">
          ${channels.length ? channels.map((channel) => `<div class="setting-row"><div><strong>${esc(channel.name)}</strong><small>${esc(channel.kind)}</small></div><div class="split"><button class="btn ghost small" type="button" data-channel-test="${esc(channel.id)}">${t('Test')}</button><button class="btn ghost small" type="button" data-channel-delete="${esc(channel.id)}">${t('Delete')}</button></div></div>`).join('') : `<p class="muted">${t('No notification channels configured.')}</p>`}
        </div>
        <button class="btn small mt-2" id="settings-channel-add" type="button">${t('Add channel')}</button>
        <p class="hint mt-1">${t('Private and loopback targets are rejected to prevent SSRF. Use a public hostname or a reverse proxy.')}</p>
      </section>
      <section class="card">
        <h2 class="mt-0">${t('Read-only API tokens')}</h2>
        <p class="hint">${t('Scoped read-only tokens for the MCP endpoint and other read clients. Tokens are shown once, stored hashed and can be revoked at any time.')}</p>
        <div class="setting-list" id="settings-token-list">
          ${tokens.length ? tokens.map((token) => `<div class="setting-row"><div><strong>${esc(token.name)}</strong><small>${esc((token.scopes || []).join(', '))} · ${token.last_used ? esc(fmtStamp(token.last_used)) : t('never used')}</small></div><button class="btn ghost small" type="button" data-token-revoke="${esc(token.id)}">${t('Revoke')}</button></div>`).join('') : `<p class="muted">${t('No API tokens.')}</p>`}
        </div>
        <button class="btn small mt-2" id="settings-token-add" type="button">${t('Create token')}</button>
        <p class="hint mt-1">${t('MCP endpoint:')} <code>/mcp</code> · ${t('Authorization: Bearer <token>')}</p>
      </section>
      <section class="card">
        <h2 class="mt-0">${isAdmin ? t('Administration') : t('About')}</h2>
        ${isAdmin ? `<div class="split wrap"><a class="btn small" href="#/users">${t('Users & roles')}</a><a class="btn small" href="#/audit">${t('Audit log')}</a><a class="btn small" href="#/diagnostics">${t('Diagnostics')}</a><a class="btn small" href="#/devices">${t('All devices')}</a></div>` : `<p class="hint">AgentNexus ${esc(state.hubName)} - ${t('version')} ${esc(state.version || t('unknown'))}. ${t('Sessions stay on your hosts; the hub stores metadata.')}</p>`}
      </section>
    </div>`;
  document.getElementById('settings-language').addEventListener('change', (event) => {
    setLang(event.target.value);
    location.reload();
  });
  view.querySelectorAll('[data-theme-choice]').forEach((button) => button.addEventListener('click', async () => {
    try {
      await saveTheme(button.dataset.themeChoice);
      view.querySelectorAll('[data-theme-choice]').forEach((other) => {
        const active = other.dataset.themeChoice === button.dataset.themeChoice;
        other.classList.toggle('active', active);
        other.setAttribute('aria-pressed', String(active));
      });
    } catch (err) { toast(err.message, true); }
  }));
  document.getElementById('settings-signout').addEventListener('click', logout);
  view.querySelectorAll('[data-notify-kind]').forEach((checkbox) => checkbox.addEventListener('change', async () => {
    const notifications = {};
    view.querySelectorAll('[data-notify-kind]').forEach((box) => { notifications[box.dataset.notifyKind] = box.checked; });
    try {
      const saved = await api('/me/settings', { method: 'PUT', body: { notifications } });
      state.settings = { ...state.settings, ...saved };
      toast(t('Notification preferences saved'));
    } catch (err) {
      checkbox.checked = !checkbox.checked;
      toast(err.message, true);
    }
  }));
  document.getElementById('settings-push')?.addEventListener('click', async () => {
    try {
      await enablePush();
      document.getElementById('settings-push-area').innerHTML = badge(t('enabled on this device'), 'online');
      toast(t('Push notifications enabled'));
    } catch (err) { toast(err.message, true); }
  });

  document.getElementById('settings-channel-add').addEventListener('click', () => {
    modal(`
      <h2>${t('Add notification channel')}</h2>
      <label class="label">${t('Kind')}<select class="select" id="channel-kind">
        ${[['ntfy', 'ntfy'], ['gotify', 'Gotify'], ['telegram', 'Telegram'], ['discord', 'Discord'], ['slack', 'Slack'], ['webhook', t('Generic webhook')]].map(([value, label]) => `<option value="${value}">${esc(label)}</option>`).join('')}
      </select></label>
      <label class="label">${t('Name')}<input class="input" id="channel-name" maxlength="80"></label>
      <label class="label" data-channel-url>${t('URL')}<input class="input" id="channel-url" placeholder="https://…"></label>
      <label class="label" data-channel-token hidden>${t('Token')}<input class="input" id="channel-token" autocomplete="off"></label>
      <label class="label" data-channel-chat hidden>${t('Chat ID')}<input class="input" id="channel-chat"></label>
      <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Cancel')}</button><button class="btn primary" type="button" id="channel-save">${t('Save')}</button></div>`);
    const kind = document.getElementById('channel-kind');
    const sync = () => {
      const value = kind.value;
      document.querySelector('[data-channel-url]').hidden = value === 'telegram';
      document.querySelector('[data-channel-token]').hidden = !['gotify', 'telegram'].includes(value);
      document.querySelector('[data-channel-chat]').hidden = value !== 'telegram';
    };
    kind.addEventListener('change', sync);
    sync();
    document.getElementById('channel-save').addEventListener('click', async () => {
      const config = {};
      const url = document.getElementById('channel-url').value.trim();
      const token = document.getElementById('channel-token').value.trim();
      const chat = document.getElementById('channel-chat').value.trim();
      if (url) config.url = url;
      if (token) config.token = token;
      if (chat) config.chat_id = chat;
      try {
        await api('/me/notification-channels', { method: 'POST', body: { kind: kind.value, name: document.getElementById('channel-name').value.trim(), config } });
        closeModal();
        toast(t('Notification channel saved'));
        await pageSettings();
      } catch (err) { toast(err.message, true); }
    });
  });
  view.querySelectorAll('[data-channel-test]').forEach((button) => button.addEventListener('click', async () => {
    try {
      await api(`/me/notification-channels/${encodeURIComponent(button.dataset.channelTest)}/test`, { method: 'POST' });
      toast(t('Test notification sent'));
    } catch (err) { toast(err.message, true); }
  }));
  view.querySelectorAll('[data-channel-delete]').forEach((button) => button.addEventListener('click', async () => {
    try {
      await api(`/me/notification-channels/${encodeURIComponent(button.dataset.channelDelete)}`, { method: 'DELETE' });
      toast(t('Notification channel deleted'));
      await pageSettings();
    } catch (err) { toast(err.message, true); }
  }));
  document.getElementById('settings-token-add').addEventListener('click', () => {
    modal(`<h2>${t('Create read-only token')}</h2>
      <label class="label">${t('Name')}<input class="input" id="token-name" maxlength="80" placeholder="${t('e.g. Claude Desktop')}"></label>
      <p class="hint">${t('The token grants read-only access to hosts, sessions, transcripts and Git status that your user may already view.')}</p>
      <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Cancel')}</button><button class="btn primary" type="button" id="token-create">${t('Create token')}</button></div>`);
    document.getElementById('token-create').addEventListener('click', async () => {
      const name = document.getElementById('token-name').value.trim();
      if (!name) { toast(t('Name is required'), true); return; }
      try {
        const result = await api('/me/api-tokens', { method: 'POST', body: { name } });
        modal(`<h2>${t('Token created')}</h2>
          <p class="hint">${t('Copy this token now. It cannot be shown again.')}</p>
          <div class="copy-line"><code id="token-secret">${esc(result.secret)}</code><button class="icon-btn" type="button" id="token-copy" title="${t('Copy')}">${svgIcon('copy', 14)}</button></div>
          <div class="modal-actions"><button class="btn primary" type="button" data-close>${t('Done')}</button></div>`);
        document.getElementById('token-copy').addEventListener('click', () => copyText(result.secret));
        document.querySelector('[data-close]')?.addEventListener('click', () => pageSettings().catch(() => {}));
      } catch (err) { toast(err.message, true); }
    });
  });
  view.querySelectorAll('[data-token-revoke]').forEach((button) => button.addEventListener('click', async () => {
    try {
      await api(`/me/api-tokens/${encodeURIComponent(button.dataset.tokenRevoke)}`, { method: 'DELETE' });
      toast(t('Token revoked'));
      await pageSettings();
    } catch (err) { toast(err.message, true); }
  }));
  const hostSelect = document.getElementById('settings-notify-host');
  if (hostSelect) {
    const hostCheckboxes = [...view.querySelectorAll('[data-host-notify-kind]')];
    const stateLabel = document.getElementById('settings-host-notify-state');
    const renderHostPrefs = () => {
      const hostID = hostSelect.value;
      const override = hostPrefs[hostID];
      const effective = { ...defaultNotificationPrefs(), ...(state.settings.notifications || {}), ...(override || {}) };
      for (const box of hostCheckboxes) box.checked = effective[box.dataset.hostNotifyKind] !== false;
      stateLabel.textContent = override ? t('Host override active') : t('Using global defaults');
    };
    const saveHostPrefs = async () => {
      const hostID = hostSelect.value;
      const notifications = {};
      for (const box of hostCheckboxes) notifications[box.dataset.hostNotifyKind] = box.checked;
      try {
        await api(`/me/settings/notifications/hosts/${encodeURIComponent(hostID)}`, { method: 'PUT', body: { notifications } });
        hostPrefs[hostID] = notifications;
        stateLabel.textContent = t('Host override active');
        toast(t('Notification preferences saved'));
      } catch (err) {
        renderHostPrefs();
        toast(err.message, true);
      }
    };
    hostSelect.addEventListener('change', renderHostPrefs);
    for (const box of hostCheckboxes) box.addEventListener('change', () => saveHostPrefs().catch((err) => toast(err.message, true)));
    document.getElementById('settings-host-notify-reset')?.addEventListener('click', async () => {
      const hostID = hostSelect.value;
      try {
        await api(`/me/settings/notifications/hosts/${encodeURIComponent(hostID)}`, { method: 'DELETE' });
        delete hostPrefs[hostID];
        renderHostPrefs();
        toast(t('Host override cleared'));
      } catch (err) { toast(err.message, true); }
    });
    renderHostPrefs();
  }
}

function defaultNotificationPrefs() {
  return { approval: true, completed: true, failed: true, disconnected: true };
}
