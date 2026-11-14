import { api } from './api.js';
import { loadUserTheme, renderShell } from './shell.js';
import { state, storeSet } from './state.js';
import { app, closeModal, copyText, modal, toast } from './ui.js';
import { disconnectWS } from './ws.js';
import { route } from './router.js';
import { getLang, setLang, t } from './i18n.js';
import { esc } from './ui-format.js';

export function authFrame(inner, subtitle) {
  app.className = 'app-loading';
  app.innerHTML = `
    <div class="auth-wrap">
      <div class="auth-lang" role="group" aria-label="${t('Language')}">
        <button type="button" data-lang="en" class="${getLang() === 'en' ? 'active' : ''}">EN</button>
        <button type="button" data-lang="de" class="${getLang() === 'de' ? 'active' : ''}">DE</button>
      </div>
      <div class="auth-card">
        <div class="auth-brand">
          <div class="brand-mark">A</div>
          <div><h1>AgentNexus</h1><p>${esc(subtitle || t('Your agents. Your machines. One place.'))}</p></div>
        </div>
        <div class="card">${inner}</div>
        <p class="small muted" style="text-align:center;margin-top:14px">${t('Self-hosted · no telemetry · transcript content stays on your hosts')}</p>
      </div>
    </div>`;
  app.querySelectorAll('[data-lang]').forEach((button) => button.addEventListener('click', () => {
    setLang(button.dataset.lang);
    location.reload();
  }));
}

export function renderAuth(mode, error = '') {
  state.me = null;
  disconnectWS();
  if (mode === 'setup') {
    authFrame(`
      <h2 class="mt-0">${t('Create the owner account')}</h2>
      <p class="hint mb-2">${t('This hub has no users yet. Choose a strong password; you can enroll a passkey or authenticator after signing in.')}</p>
      ${error ? `<div class="notice error mb-2">${esc(error)}</div>` : ''}
      <form class="form" id="setup-form">
        <label class="label">${t('Username')}<input class="input" name="username" aria-label="${t('Username')}" autocomplete="username" minlength="3" maxlength="64" required></label>
        <label class="label">${t('Password')}<input class="input" name="password" aria-label="${t('Password')}" type="password" autocomplete="new-password" minlength="10" required></label>
        <button class="btn primary block" type="submit" id="auth-submit">${t('Create hub')}</button>
      </form>`, t('First-run setup'));
    document.getElementById('setup-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const form = new FormData(event.target);
      const button = document.getElementById('auth-submit');
      button.disabled = true;
      button.innerHTML = `<span class="spinner"></span> ${t('Creating...')}`;
      try {
        const data = await api('/setup', { method: 'POST', body: {
          username: form.get('username'), password: form.get('password'),
        }, allow401: true });
        state.me = await api('/me');
        state.hubName = state.me.hub_name || state.hubName;
      await loadUserTheme();
        renderShell('overview');
        await route();
        if (data.totp_secret) {
          modal(`
            <h2>${t('Save your authenticator secret')}</h2>
            <p class="hint">${t('TOTP is optional until you confirm it. Scan this URI with an authenticator app, or copy the secret. You can enrol later in Devices.').replace('Devices', `<b>${t('Devices')}</b>`)}</p>
            <div class="secret">${esc(data.totp_secret)}</div>
            <p class="small muted mt-1">${t('otpauth URI')}</p>
            <div class="secret">${esc(data.totp_url)}</div>
            <div class="modal-actions">
              <button class="btn" type="button" id="copy-totp">${t('Copy secret')}</button>
              <button class="btn primary" type="button" data-close>${t('I saved it')}</button>
            </div>`);
          document.getElementById('copy-totp').addEventListener('click', () => copyText(data.totp_secret));
        }
      } catch (err) {
        renderAuth('setup', err.message);
      }
    });
    return;
  }

  authFrame(`
    <h2 class="mt-0">${t('Sign in')}</h2>
    <p class="hint mb-2">${t('Use your owner account. Authenticator code is required once TOTP is enabled.')}</p>
    ${error ? `<div class="notice error mb-2">${esc(error)}</div>` : ''}
    <form class="form" id="login-form">
      <label class="label">${t('Username')}<input class="input" name="username" aria-label="${t('Username')}" autocomplete="username" required autofocus></label>
      <label class="label">${t('Password')}<input class="input" name="password" aria-label="${t('Password')}" type="password" autocomplete="current-password" required></label>
      <label class="label">${t('Authenticator code')} <span class="muted">${t('(if enabled)')}</span><input class="input" name="totp" aria-label="${t('Authenticator code')}" inputmode="numeric" autocomplete="one-time-code" maxlength="6" pattern="[0-9]*"></label>
      <button class="btn primary block" type="submit" id="auth-submit">${t('Sign in')}</button>
      <button class="btn block" type="button" id="passkey-login">${t('Sign in with a passkey')}</button>
    </form>`, t('Sign in to your hub'));

  document.getElementById('passkey-login')?.addEventListener('click', async () => {
    const username = document.querySelector('[name=username]')?.value.trim();
    if (!username) { toast(t('Enter your username first'), true); return; }
    try {
      await passkeyLogin(username);
      state.me = await api('/me');
      state.hubName = state.me.hub_name || state.hubName;
      await loadUserTheme();
      renderShell('overview');
      await route();
    } catch (err) {
      renderAuth('login', err.message);
    }
  });

  document.getElementById('login-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = new FormData(event.target);
    const button = document.getElementById('auth-submit');
    button.disabled = true;
    button.innerHTML = `<span class="spinner"></span> ${t('Signing in...')}`;
    try {
      await api('/auth/login', { method: 'POST', body: {
        username: form.get('username'), password: form.get('password'), totp: form.get('totp') || '',
      }, allow401: true });
      state.me = await api('/me');
      state.hubName = state.me.hub_name || state.hubName;
      await loadUserTheme();
      renderShell('overview');
      await route();
    } catch (err) {
      renderAuth('login', err.message);
    }
  });
}

export async function logout() {
  try { await api('/auth/logout', { method: 'POST' }); } catch { /* ignore */ }
  state.me = null;
  state.openTabs = [];
  storeSet('nexus-tabs', []);
  if (state.notifTimer) {
    clearInterval(state.notifTimer);
    state.notifTimer = null;
  }
  disconnectWS();
  renderAuth('login');
}

export function renderDevicePair(token) {
  authFrame(`
    <h2 class="mt-0">${t('Pair this device')}</h2>
    <p class="hint mb-2">${t('A signed-in device requested this pairing. Give this browser a name and confirm; the link is single-use.')}</p>
    <form class="form" id="pair-form">
      <label class="label">${t('Device name')}<input class="input" name="name" placeholder="${t('Phone, tablet, second laptop...')}" required maxlength="80"></label>
      <button class="btn primary block" type="submit">${t('Pair this device')}</button>
    </form>`, t('Device pairing'));
  document.getElementById('pair-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const name = new FormData(event.target).get('name');
    try {
      await api('/auth/device-pair', { method: 'POST', body: { token, name }, allow401: true });
      state.me = await api('/me');
      state.hubName = state.me.hub_name || state.hubName;
      await loadUserTheme();
      history.replaceState({}, '', '/');
      renderShell('overview');
      await route();
    } catch (err) {
      renderDevicePair(token);
      toast(err.message, true);
    }
  });
}

export function urlBase64ToUint8Array(base64String) {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(base64);
  const output = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) output[i] = raw.charCodeAt(i);
  return output;
}

export function b64urlToBuf(value) {
  const normalized = String(value).replace(/-/g, '+').replace(/_/g, '/');
  const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4);
  const raw = atob(padded);
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}

export function bufToB64url(buffer) {
  const bytes = new Uint8Array(buffer);
  let s = '';
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

export function prepareCreation(publicKey) {
  const pk = { ...publicKey };
  pk.challenge = b64urlToBuf(pk.challenge);
  if (pk.user) pk.user = { ...pk.user, id: b64urlToBuf(pk.user.id) };
  if (Array.isArray(pk.excludeCredentials)) {
    pk.excludeCredentials = pk.excludeCredentials.map((c) => ({ ...c, id: b64urlToBuf(c.id) }));
  }
  return pk;
}

export function prepareAssertion(publicKey) {
  const pk = { ...publicKey };
  pk.challenge = b64urlToBuf(pk.challenge);
  if (Array.isArray(pk.allowCredentials)) {
    pk.allowCredentials = pk.allowCredentials.map((c) => ({ ...c, id: b64urlToBuf(c.id) }));
  }
  return pk;
}

export function serializeCredential(cred, kind) {
  const response = kind === 'create'
    ? {
        clientDataJSON: bufToB64url(cred.response.clientDataJSON),
        attestationObject: bufToB64url(cred.response.attestationObject),
        transports: cred.response.getTransports ? cred.response.getTransports() : undefined,
      }
    : {
        clientDataJSON: bufToB64url(cred.response.clientDataJSON),
        authenticatorData: bufToB64url(cred.response.authenticatorData),
        signature: bufToB64url(cred.response.signature),
        userHandle: cred.response.userHandle ? bufToB64url(cred.response.userHandle) : null,
      };
  return {
    id: cred.id,
    rawId: bufToB64url(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment || undefined,
    response,
    clientExtensionResults: cred.getClientExtensionResults(),
  };
}

export async function registerPasskey() {
  if (!window.PublicKeyCredential) throw new Error('WebAuthn is not supported by this browser');
  const begin = await api('/auth/passkey/register/begin', { method: 'POST' });
  const cred = await navigator.credentials.create({ publicKey: prepareCreation(begin.publicKey) });
  if (!cred) throw new Error('Passkey creation was cancelled');
  await api(`/auth/passkey/register/finish?session=${encodeURIComponent(begin.session_id)}&name=${encodeURIComponent('This device')}`, {
    method: 'POST',
    body: serializeCredential(cred, 'create'),
  });
}

export async function passkeyLogin(username) {
  if (!window.PublicKeyCredential) throw new Error('WebAuthn is not supported by this browser');
  const begin = await api('/auth/passkey/begin', { method: 'POST', body: { username }, allow401: true });
  const cred = await navigator.credentials.get({ publicKey: prepareAssertion(begin.publicKey) });
  if (!cred) throw new Error('Passkey sign-in was cancelled');
  await api(`/auth/passkey/finish?session=${encodeURIComponent(begin.session_id)}`, {
    method: 'POST',
    body: serializeCredential(cred, 'get'),
    allow401: true,
  });
}

export function ensureStepUp() {
  if (!stepUpFlight) {
    stepUpFlight = performStepUp().finally(() => { stepUpFlight = null; });
  }
  return stepUpFlight;
}

export async function performStepUp() {
  const status = await api('/auth/step-up');
  if (status.valid_until && new Date(status.valid_until) > new Date()) return;
  const begin = await api('/auth/step-up/begin', { method: 'POST' });
  if (begin.method === 'passkey') {
    if (!window.PublicKeyCredential) throw new Error('WebAuthn is not supported by this browser');
    const cred = await navigator.credentials.get({ publicKey: prepareAssertion(begin.publicKey) });
    if (!cred) throw new Error('Step-up confirmation was cancelled');
    await api(`/auth/step-up/passkey?session=${encodeURIComponent(begin.session_id)}`, {
      method: 'POST',
      body: serializeCredential(cred, 'get'),
      allow401: true,
    });
  } else {
    await stepUpPasswordDialog(begin.totp_enabled);
  }
  toast(t('Confirmed'));
}

export function stepUpPasswordDialog(totpEnabled) {
  const hint = totpEnabled
    ? t('This change needs a fresh confirmation. Enter your password and authenticator code.')
    : t('This change needs a fresh confirmation. Enter your password.');
  return new Promise((resolve, reject) => {
    modal(`
      <h2>${t("Confirm it's you")}</h2>
      <p class="hint">${esc(hint)}</p>
      <form class="form" id="step-up-form">
        <label class="label">${t('Password')}<input class="input" type="password" name="password" required autocomplete="current-password"></label>
        ${totpEnabled ? `<label class="label">${t('One-time code')}<input class="input" name="totp" inputmode="numeric" autocomplete="one-time-code" required></label>` : ''}
        <div class="modal-actions">
          <button class="btn ghost" type="button" data-close>${t('Cancel')}</button>
          <button class="btn primary" type="submit">${t('Confirm')}</button>
        </div>
      </form>`, () => reject(new Error('step-up cancelled')));
    document.getElementById('step-up-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const form = new FormData(event.target);
      try {
        await api('/auth/step-up/password', {
          method: 'POST',
          body: { password: form.get('password'), totp: form.get('totp') || '' },
          allow401: true,
        });
        resolve();
        closeModal();
      } catch (err) {
        toast(err.message, true);
      }
    });
  });
}

export let stepUpFlight = null;

export async function enablePush() {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    throw new Error('Push is not supported by this browser');
  }
  const registration = await navigator.serviceWorker.register('/sw.js');
  const permission = await Notification.requestPermission();
  if (permission !== 'granted') throw new Error('Notification permission was not granted');
  const key = await api('/push/key');
  const subscription = await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(key.public_key),
  });
  const json = subscription.toJSON();
  await api('/push/subscribe', { method: 'POST', body: {
    endpoint: json.endpoint,
    keys: { p256dh: json.keys.p256dh, auth: json.keys.auth },
  } });
}
