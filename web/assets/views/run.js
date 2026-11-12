import { api } from '../api.js';
import { rememberRecent } from '../palette.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { openTab } from '../tabs.js';
import { agentGlyph, b64decode, stateBadge, toast } from '../ui.js';
import { wsSend } from '../ws.js';
import { pageStructuredRun } from './chat.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { agentMeta, esc, projectName, shortId } from '../ui-format.js';

export async function pageRun(id) {
  const pageGen = state.pageGen;
  renderShell('sessions');
  const run = await api(`/runs/${id}`);
  if (pageGen !== state.pageGen) return;
  state.run = run;
  if (run.mode === 'structured') return pageStructuredRun(id, run);
  const title = run.session_title || t('Terminal');
  openTab({ id, key: run.session_id || id, kind: 'run', href: `#/runs/${id}`, title, agent: run.agent });
  rememberRecent({ id, title, agent: run.agent, hint: `${agentMeta(run.agent).label} · ${run.host_name || run.host_id} · terminal`, href: `#/runs/${id}` });
  setPage(title, `Terminal · ${projectName(run.cwd)} · ${run.host_name || run.host_id}`, `<button class="btn small danger" id="stop-run">${svgIcon('stop')} ${t('Stop')}</button><a class="btn small ghost" href="#/sessions">${t('Sessions')}</a>`);
  const view = document.getElementById('view');
  view.innerHTML = `
    <div class="terminal-page">
      <div class="terminal-statusbar">
        ${stateBadge(run.state)}
        ${run.error_code ? `<span class="badge failed" title="${esc(run.error_message || '')}">${esc(run.error_code)}</span>` : ''}
        ${agentGlyph(run.agent)}
        <span>${esc(agentMeta(run.agent).label)}</span><span class="chat-sep">·</span><span>${esc(projectName(run.cwd))}</span><span class="chat-sep">·</span><span>${esc(run.host_name || run.host_id)}</span>
        <span class="chat-connection right" id="run-note"><span class="connection-dot"></span>Connecting…</span>
      </div>
      <div class="terminal-shell">
        <div class="terminal-bar"><span class="dot" id="term-dot"></span><span id="term-status">${t('disconnected')}</span><span class="right mono-small">${t('lease')} ${esc(shortId(run.lease_device_id || 'none'))} · ${esc(run.id)}</span></div>
        <div class="terminal" id="terminal"></div>
        <div class="keybar">
          <button class="btn small" data-key="esc">Esc</button>
          <button class="btn small" data-key="tab">Tab</button>
          <button class="btn small" data-key="ctrl-c">Ctrl-C</button>
          <button class="btn small" data-key="up">↑</button>
          <button class="btn small" data-key="down">↓</button>
          <button class="btn small" data-key="left">←</button>
          <button class="btn small" data-key="right">→</button>
          <button class="btn small" data-key="enter">Enter</button>
        </div>
      </div>
    </div>`;
  const term = new Terminal({
    cursorBlink: true,
    fontFamily: 'JetBrains Mono, Cascadia Code, SFMono-Regular, Consolas, monospace',
    fontSize: 13,
    lineHeight: 1.25,
    theme: {
      background: '#0f1114', foreground: '#e7e8ea', cursor: '#7d8cf8', selectionBackground: '#2a3352',
      black: '#15171b', red: '#e06c75', green: '#7bbf8a', yellow: '#d6b26f', blue: '#7d9df8',
      magenta: '#b49af8', cyan: '#7cc7c7', white: '#e7e8ea',
    },
    scrollback: 10000,
    allowProposedApi: false,
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(document.getElementById('terminal'));
  fit.fit();
  const dot = document.getElementById('term-dot');
  const status = document.getElementById('term-status');
  const note = document.getElementById('run-note');
  const setLive = (text) => { status.textContent = t(text); dot.classList.toggle('live', text === 'connected'); };
  let lastSeq = 0;
  let subscribedGen = 0;
  let runEpoch = '';
  const input = (data) => {
    if (run.lease_device_id && state.me.device.id && run.lease_device_id !== state.me.device.id) {
      note.textContent = t('Read-only: another device holds the lease.');
      return;
    }
    wsSend({ type: 'input', run_id: id, data: btoa(String.fromCharCode(...new TextEncoder().encode(data))) });
  };
  term.onData(input);
  const resize = () => {
    fit.fit();
    wsSend({ type: 'resize', run_id: id, cols: term.cols, rows: term.rows });
  };
  const onResize = () => resize();
  window.addEventListener('resize', onResize);
  const ro = new ResizeObserver(() => resize());
  ro.observe(document.getElementById('terminal'));

  api(`/runs/${id}/lease`, { method: 'POST' }).then(() => {
    run.lease_device_id = state.me.device.id;
    note.textContent = t('You have control (controller lease).');
  }).catch((err) => { note.textContent = t('Read-only:') + ' ' + err.message; });

  document.getElementById('stop-run').addEventListener('click', async () => {
    try { await api(`/runs/${id}/stop`, { method: 'POST' }); toast('Stopping run'); }
    catch (err) { toast(err.message, true); }
  });

  const sendSubscribe = () => {
    if (wsSend({ type: 'subscribe', run_id: id, last_seq: lastSeq })) subscribedGen = state.wsGen;
  };
  const handler = (msg) => {
    if (msg.run_id !== id && msg.type !== 'ready') return;
    switch (msg.type) {
      case 'run.output':
        if (msg.data) {
          term.write(b64decode(msg.data));
          lastSeq = Math.max(lastSeq, msg.seq || 0);
        }
        break;
      case 'run.state':
        if (msg.epoch && runEpoch && msg.epoch !== runEpoch) {
          lastSeq = 0;
          sendSubscribe();
        }
        if (msg.epoch) runEpoch = msg.epoch;
        if (msg.state) { run.state = msg.state; note.textContent = `state: ${msg.state}`; }
        break;
      case 'run.exit':
        run.state = msg.state || 'exited';
        setLive('exited');
        note.textContent = msg.message || 'agent exited';
        term.write(`\r\n\x1b[2m[run ${run.state}]\x1b[0m\r\n`);
        break;
      case 'run.error':
        note.textContent = msg.error || 'run failed';
        term.write(`\r\n\x1b[31m[${msg.error || 'run failed'}]\x1b[0m\r\n`);
        break;
      case 'run.lease':
        note.textContent = 'Lease transferred to ' + (msg.message || 'another device');
        break;
    }
  };
  state.wsHandlers.add(handler);
  // The websocket may still be connecting, and it can reconnect later. Keep
  // re-subscribing on each new socket generation so buffered output is
  // replayed from lastSeq (FR-27).
  sendSubscribe();
  const timer = setInterval(() => {
    if (!state.me) { clearInterval(timer); return; }
    if (state.ws?.readyState === WebSocket.OPEN) {
      if (subscribedGen !== state.wsGen) sendSubscribe();
      setLive('connected');
    } else {
      setLive('reconnecting');
    }
  }, 500);
  state.pageCleanup = () => {
    clearInterval(timer);
    state.wsHandlers.delete(handler);
    window.removeEventListener('resize', onResize);
    ro.disconnect();
    term.dispose();
  };
  document.querySelectorAll('[data-key]').forEach((b) => b.addEventListener('click', () => {
    const keys = { esc: '\x1b', tab: '\t', 'ctrl-c': '\x03', up: '\x1b[A', down: '\x1b[B', left: '\x1b[D', right: '\x1b[C', enter: '\r' };
    input(keys[b.dataset.key] || '');
    term.focus();
  }));
  term.focus();
}
