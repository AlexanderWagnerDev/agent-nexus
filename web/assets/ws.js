/* Browser WebSocket with jittered exponential backoff. */
import { state } from './state.js';

/** Base delay before the next reconnect attempt. */
export function backoffBase(retry) {
  return Math.min(retry, 15000);
}

/** Next retry budget; grows by 1.7x and is capped at 15 seconds. */
export function nextBackoff(retry) {
  return Math.min(retry * 1.7, 15000);
}

/** Jittered delay in milliseconds, between 80% and 120% of the base. */
export function backoffDelay(retry, random = Math.random) {
  return Math.round(backoffBase(retry) * (0.8 + random() * 0.4));
}

export function connectWS() {
  if (!state.me) return;
  if (state.ws && (state.ws.readyState === WebSocket.OPEN || state.ws.readyState === WebSocket.CONNECTING)) return;
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${proto}://${location.host}/api/v1/ws`);
  state.ws = ws;
  ws.addEventListener('open', () => { state.wsRetry = 1000; state.wsGen++; });
  ws.addEventListener('message', (event) => {
    let msg;
    try { msg = JSON.parse(event.data); } catch { return; }
    if (msg.type === 'ready') return;
    for (const handler of state.wsHandlers) {
      try { handler(msg); } catch (err) { console.error(err); }
    }
  });
  ws.addEventListener('close', () => {
    state.ws = null;
    if (!state.me) return;
    // Jitter avoids a thundering herd when a hub restarts and every open
    // browser reconnects at once.
    setTimeout(connectWS, backoffDelay(state.wsRetry));
    state.wsRetry = nextBackoff(state.wsRetry);
  });
  ws.addEventListener('error', () => ws.close());
}

export function disconnectWS() {
  if (state.ws) {
    state.ws.onclose = null;
    state.ws.close();
    state.ws = null;
  }
}

export function wsSend(msg) {
  if (state.ws?.readyState === WebSocket.OPEN) {
    state.ws.send(JSON.stringify(msg));
    return true;
  }
  return false;
}
