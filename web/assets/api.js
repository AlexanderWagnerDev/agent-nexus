/* Fetch wrapper with CSRF marker, JSON decoding and step-up retry. */
import { state } from './state.js';
import { disconnectWS } from './ws.js';

let unauthorizedHook = null;
let stepUpHook = null;

/** Registered by the bootstrap so a 401 returns to the login screen. */
export function setUnauthorizedHook(fn) { unauthorizedHook = fn; }

/** Registered by the bootstrap so a step-up challenge can be retried. */
export function setStepUpHook(fn) { stepUpHook = fn; }

export async function api(path, options = {}) {
  const { method = 'GET', body, allow401 = false, stepUpRetry = false } = options;
  const headers = { 'X-Nexus-Request': '1', Accept: 'application/json' };
  const init = { method, headers, credentials: 'same-origin' };
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  const res = await fetch('/api/v1' + path, init);
  const text = await res.text();
  let data = {};
  if (text) {
    try { data = JSON.parse(text); } catch { data = { error: text }; }
  }
  if (!res.ok) {
    if (res.status === 403 && data.step_up_required && !stepUpRetry) {
      await stepUpHook?.();
      return api(path, { ...options, stepUpRetry: true });
    }
    const err = new Error(data.error || `${res.status} ${res.statusText}`);
    err.status = res.status;
    if (res.status === 401 && !allow401) {
      state.me = null;
      disconnectWS();
      unauthorizedHook?.();
    }
    throw err;
  }
  return data;
}
