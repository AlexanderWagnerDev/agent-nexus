import { api } from '../api.js';
import { enablePush, registerPasskey } from '../auth.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { app, badge, closeModal, confirmDanger, copyText, modal, toast } from '../ui.js';
import { t } from '../i18n.js';
import { esc, fmtTime, shortId } from '../ui-format.js';

export async function pageDevices() {
  const pageGen = state.pageGen;
  renderShell('devices');
  const view = setPage(t('Devices'), t('Signed-in browsers. Revoke anything you no longer trust; revocation ends its session immediately.'), `<button class="btn primary small" id="pair-device">${t('Pair a device')}</button>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const data = await api('/devices');
  if (pageGen !== state.pageGen) return;
  view.innerHTML = `
    <div class="table-wrap">
      <table class="devices-table"><thead><tr><th>${t('Device')}</th><th>${t('Created')}</th><th>${t('Last seen')}</th><th></th></tr></thead><tbody>
      ${data.devices.map((d) => `<tr>
        <td><b>${esc(d.name)}</b><div class="mono-small">${esc(shortId(d.id))}</div></td>
        <td>${esc(fmtTime(d.created_at))}</td>
        <td>${esc(fmtTime(d.last_seen))}</td>
        <td class="nowrap">${d.id === data.current_device_id ? badge('this device', 'online') : `<button class="btn small danger" data-revoke-device="${esc(d.id)}">${t('Revoke')}</button>`}</td>
      </tr>`).join('')}
      </tbody></table>
    </div>
    <div class="card mt-3">
      <h2 class="mt-0">${t('Authenticator (TOTP)')}</h2>
      <p class="hint">${state.me.totp_enabled ? t('TOTP is enabled. A code is required at every login.') : t('TOTP is not enabled yet. Enrolling it adds a second factor to password login.')}</p>
      <div id="totp-area">
        ${state.me.totp_enabled ? badge(t('enabled'), 'online') : `<button class="btn" id="totp-begin">${t('Set up authenticator')}</button>`}
      </div>
    </div>
    <div class="card mt-2">
      <h2 class="mt-0">${t('Passkeys')}</h2>
      <p class="hint">${t('Passkeys use WebAuthn and are phishing-resistant. Add one to this device, then use “Sign in with a passkey” on the login page.')}</p>
      <div id="passkey-area"><button class="btn" id="add-passkey">${t('Add a passkey')}</button></div>
    </div>
    <div class="card mt-2">
      <h2 class="mt-0">${t('Web Push notifications')}</h2>
      <p class="hint">${t('Content-free alerts for run completion and approval requests. The payload never contains transcript text, commands or file paths.')}</p>
      <div id="push-area"><button class="btn" id="enable-push">${t('Enable notifications')}</button></div>
    </div>
    <p class="small muted mt-2">${t('Passkey login and Argon2id passwords with optional TOTP are available. Session cookies are HttpOnly, SameSite=Strict and Secure when TLS is configured.')}</p>`;
  view.querySelectorAll('[data-revoke-device]').forEach((b) => b.addEventListener('click', () => {
    confirmDanger('Revoking this device ends all of its signed-in sessions.', async () => {
      await api(`/devices/${b.dataset.revokeDevice}`, { method: 'DELETE' });
      toast(t('Device revoked'));
      pageDevices();
    });
  }));
  document.getElementById('pair-device')?.addEventListener('click', async () => {
    try {
      const data = await api('/devices/pairing', { method: 'POST' });
      modal(`
        <h2>${t('Pair a device')}</h2>
        <p class="hint">${t('Scan this code on a new device. It is single-use and expires in two minutes. The new device gets its own revocable session.')}</p>
        <div style="text-align:center"><img src="${data.qr}" alt="Device pairing QR code" style="width:min(320px,100%);border-radius:12px;background:#fff;padding:8px"></div>
        <div class="secret mt-2">${esc(data.url)}</div>
        <div class="modal-actions">
          <button class="btn" type="button" id="copy-pair">${t('Copy link')}</button>
          <button class="btn primary" type="button" data-close>${t('Done')}</button>
        </div>`);
      document.getElementById('copy-pair').addEventListener('click', () => copyText(data.url));
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('add-passkey')?.addEventListener('click', async () => {
    try {
      await registerPasskey();
      document.getElementById('passkey-area').innerHTML = badge(t('passkey added'), 'online');
      toast(t('Passkey added'));
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('enable-push')?.addEventListener('click', async () => {
    try {
      await enablePush();
      document.getElementById('push-area').innerHTML = badge('enabled on this device', 'online');
      toast('Push notifications enabled');
    } catch (err) {
      toast(err.message, true);
    }
  });
  document.getElementById('totp-begin')?.addEventListener('click', async () => {
    try {
      const data = await api('/auth/totp/begin', { method: 'POST' });
      modal(`
        <h2>${t('Set up authenticator')}</h2>
        <p class="hint">${t('Add this secret to your authenticator app, then enter the current six-digit code to confirm.')}</p>
        <div class="secret">${esc(data.secret)}</div>
        <div class="secret mt-1">${esc(data.url)}</div>
        <form class="form mt-2" id="totp-form">
          <label class="label">${t('Current code')}<input class="input" name="code" inputmode="numeric" maxlength="6" pattern="[0-9]{6}" required></label>
          <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Cancel')}</button><button class="btn primary" type="submit">${t('Enable TOTP')}</button></div>
        </form>`);
      document.getElementById('totp-form').addEventListener('submit', async (e) => {
        e.preventDefault();
        try {
          await api('/auth/totp/enable', { method: 'POST', body: { secret: data.secret, code: new FormData(e.target).get('code') } });
          closeModal();
          state.me = await api('/me');
          toast(t('TOTP enabled'));
          pageDevices();
        } catch (err) { toast(err.message, true); }
      });
    } catch (err) { toast(err.message, true); }
  });
}
