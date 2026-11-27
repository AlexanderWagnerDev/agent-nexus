import { api } from '../api.js';
import { state, storeGet, storeSet } from '../state.js';
import { agentGlyph } from '../ui.js';
import { describeTool } from '../chat-format.js';
import { diffFileList, diffSummary, parseUnifiedDiff, renderSplitDiff, renderUnifiedDiff } from '../diff.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { highlightCode, renderMarkdown } from '../markdown.js';
import { agentMeta, countLabel, esc, fmtStamp } from '../ui-format.js';

const DETAIL_KEY = 'nexus-transcript-detail';
const DETAIL_LEVELS = ['chat', 'normal', 'full'];

/** How much of a transcript is rendered: chat only, normal or full. The hub
 * stores the preference per user; localStorage keeps it steady until the
 * settings request returns and on hubs that predate the column. */
export function transcriptDetail() {
  const hub = state.settings?.transcript_detail;
  if (DETAIL_LEVELS.includes(hub)) return hub;
  const level = storeGet(DETAIL_KEY, 'normal');
  return DETAIL_LEVELS.includes(level) ? level : 'normal';
}

export function setTranscriptDetail(level) {
  const value = DETAIL_LEVELS.includes(level) ? level : 'normal';
  state.settings = { ...state.settings, transcript_detail: value };
  storeSet(DETAIL_KEY, value);
}

/** Options for a compact detail selector that mirrors the settings page. */
export function transcriptDetailOptions() {
  return [['chat', 'Chat only'], ['normal', 'Normal'], ['full', 'Full']]
    .map(([value, label]) => `<option value="${value}" ${transcriptDetail() === value ? 'selected' : ''}>${t(label)}</option>`)
    .join('');
}

/** Save the choice on the hub, restoring the previous value on failure. */
export async function saveTranscriptDetail(level) {
  const previous = transcriptDetail();
  setTranscriptDetail(level);
  try {
    const saved = await api('/me/settings', { method: 'PUT', body: { transcript_detail: transcriptDetail() } });
    state.settings = { ...state.settings, ...saved };
  } catch (err) {
    setTranscriptDetail(previous);
    throw err;
  }
}

export function inlineAttachmentLabel(name, path) {
  const raw = String(name || '').trim();
  if (raw && !/^\[image #\d+\]$/i.test(raw)) return raw;
  const clean = String(path || '').replaceAll('\\', '/');
  return (clean.split('/').filter(Boolean).pop() || 'attachment').replace(/^\d{2}-/, '');
}

export function renderUserText(raw) {
  let text = String(raw || '');
  const attachments = [];
  text = text.replace(/<image\b[^>]*>/gi, (tag) => {
    const name = (tag.match(/name="([^"]*)"/i) || [])[1] || '';
    const file = (tag.match(/path="([^"]*)"/i) || [])[1] || '';
    attachments.push(inlineAttachmentLabel(name, file));
    return '';
  });
  text = text.replace(/<\/image>/gi, '');
  text = text.replace(/<(mention|skill)\b[^>]*\bname="([^"]*)"[^>]*>/gi, (_, _kind, name) => {
    attachments.push(name);
    return '';
  });
  text = text.replace(/<\/?mention[^>]*>/gi, '').replace(/<\/?skill[^>]*>/gi, '').trim();
  const chips = attachments.map((name) => `<span class="inline-attachment">${svgIcon('paperclip', 12)}${esc(name)}</span>`).join('');
  if (!text && !chips) return '';
  return `${chips}${text ? `<div class="user-text">${esc(text)}</div>` : ''}`;
}

export function renderTranscriptMessage(m, agent = 'agent') {
  const detail = transcriptDetail();
  if (detail !== 'full' && m.role === 'user' && /^\s*<(environment_context|recommended_plugins|user_instructions|permissions|skills)/i.test(m.text || '')) return '';
  if (m.kind === 'tool_call' || m.kind === 'tool_result') {
    const described = describeTool(m.tool_name || (m.kind === 'tool_result' ? 'Tool result' : 'Tool'), m.tool_input || '', m.tool_output || '', m.tool_call_id || '');
    return historyToolCard(described, m, detail === 'chat');
  }
  if (m.kind === 'reasoning' && m.text) {
    return `<article class="turn turn-assistant turn-reasoning"${detail === 'chat' ? ' hidden' : ''}>${reasoningHtml(m.text, detail === 'full')}</article>`;
  }
  if (m.role === 'user') {
    const body = renderUserText(m.text);
    if (!body) return '';
    return `<article class="turn turn-user"><div class="turn-meta"><span>${t('You')}</span><time>${esc(fmtStamp(m.timestamp))}</time></div><div class="turn-body">${body}</div></article>`;
  }
  return `<article class="turn turn-assistant"><div class="turn-meta">${agentGlyph(agent, 'small')}<span class="agent-name">${esc(agentMeta(agent).label)}</span><time>${esc(fmtStamp(m.timestamp))}</time></div><div class="turn-body md">${renderMarkdown(m.text || '')}</div></article>`;
}

export function renderTranscriptMessages(messages, agent = 'agent') {
  const html = [];
  const detail = transcriptDetail();
  for (let i = 0; i < messages.length; i += 1) {
    const message = messages[i];
    if (message.kind === 'tool_call') {
      const next = messages[i + 1];
      // OpenCode 2.x stores the output on the tool call itself; older stores
      // emit a separate tool_result row immediately after it.
      const output = next?.kind === 'tool_result' ? (next.tool_output || '') : (message.tool_output || '');
      if (next?.kind === 'tool_result') i += 1;
      const described = describeTool(message.tool_name || 'Tool', message.tool_input || '', output, message.tool_call_id || '');
      html.push(historyToolCard(described, { ...message, tool_output: output }, detail === 'chat'));
    } else if (message.kind === 'tool_result') {
      const described = describeTool(message.tool_name || 'Tool result', '', message.tool_output || '', message.tool_call_id || '');
      html.push(historyToolCard(described, message, detail === 'chat'));
    } else {
      html.push(renderTranscriptMessage(message, agent));
    }
  }
  return html.join('');
}

export function toolIcon(kind) {
  return { terminal: 'terminal', read: 'file', edit: 'edit', write: 'edit', search: 'searchFile', web: 'web', git: 'git', test: 'check', todo: 'plan', task: 'task', mcp: 'mcp', image: 'image' }[kind] || 'tool';
}

export function renderDiffBlock(text, title = 'diff') {
  const files = parseUnifiedDiff(text);
  if (!files.length) {
    return `<div class="md-code diff-view"><div class="md-code-head"><span class="md-code-lang">diff</span><button class="md-copy" type="button">${t('Copy')}</button></div><pre><code>${highlightCode(String(text || ''), 'diff')}</code></pre></div>`;
  }
  const summary = diffSummary(files);
  const mode = storeGet('nexus-diff-mode', 'unified');
  return `<div class="diff-panel" data-mode="${mode}">
    <div class="diff-toolbar">
      <span class="diff-summary">${countLabel(summary.files, 'file', 'files')} <b>+${summary.additions}</b> <i>-${summary.deletions}</i></span>
      <div class="diff-mode-switch" role="group" aria-label="${t('Diff layout')}">
        <button type="button" data-diff-mode="unified" class="${mode === 'unified' ? 'active' : ''}">${t('Unified')}</button>
        <button type="button" data-diff-mode="split" class="${mode === 'split' ? 'active' : ''}">${t('Split')}</button>
      </div>
    </div>
    <div class="diff-file-list">${diffFileList(files)}</div>
    <div class="diff-body">
      <div class="diff-unified">${renderUnifiedDiff(files, { title })}</div>
      <div class="diff-split">${renderSplitDiff(files)}</div>
    </div>
  </div>`;
}

export function applyDiffMode(panel, mode) {
  if (!panel) return;
  panel.dataset.mode = mode;
  storeSet('nexus-diff-mode', mode);
  panel.querySelectorAll('[data-diff-mode]').forEach((button) => {
    button.classList.toggle('active', button.dataset.diffMode === mode);
  });
}

export function outputBlock(text) {
  const value = String(text || '');
  if (!value) return '';
  if (/^(diff --git|@@ )/m.test(value) && /^[+-]/m.test(value)) return renderDiffBlock(value);
  return `<pre class="tool-output">${esc(value)}</pre>`;
}

export function toolCardHtml(described, status = 'done', hidden = false) {
  const detail = described.detail ? `<code>${esc(described.detail)}</code>` : '';
  const statusMark = status === 'running'
    ? `<span class="spinner tiny"></span>${t('Running')}`
    : status === 'error'
      ? `${svgIcon('alert', 13)}${t('Failed')}`
      : `${svgIcon('check', 13)}${t('Done')}`;
  const commandBlock = described.command && described.kind === 'terminal'
    ? `<div class="tool-block"><div class="tool-label">${t('Command')}</div><pre class="tool-pre">${esc(described.command)}</pre></div>`
    : '';
  const pathBlock = described.path && described.kind !== 'terminal' && described.kind !== 'edit' && described.kind !== 'write'
    ? `<div class="tool-block"><div class="tool-label">${t('Path')}</div><pre class="tool-pre">${esc(described.path)}</pre></div>`
    : '';
  const diffBlock = described.diff ? renderDiffBlock(described.diff) : '';
  const output = described.kind !== 'edit' && described.output ? `<div class="tool-block"><div class="tool-label">${t('Output')}</div>${outputBlock(described.output)}</div>` : '';
  const hasStructured = commandBlock || pathBlock || diffBlock || output;
  const fullButton = described.truncated && described.callID
    ? `<button class="btn ghost small tool-load-output" type="button" data-load-output="${esc(described.callID)}">${t('Load full output')}</button>`
    : '';
  const raw = described.inputText && described.inputText !== '{}' && described.inputText !== 'null'
    ? `<details class="tool-raw" ${transcriptDetail() === 'full' ? 'open' : ''}><summary>${t('Raw input')}</summary><pre>${esc(described.inputText)}</pre></details>`
    : '';
  return `<details class="tool-card ${status}"${hidden ? ' hidden' : ''} ${status === 'error' || transcriptDetail() === 'full' ? 'open' : ''}>
    <summary>
      <span class="tool-icon">${svgIcon(toolIcon(described.kind), 15)}</span>
      <span class="tool-line"><strong>${esc(described.label)}</strong>${detail}</span>
      <span class="tool-state ${status}">${statusMark}</span>
      <span class="tool-chevron">${svgIcon('chevronDown', 14)}</span>
    </summary>
    <div class="tool-body">${hasStructured ? commandBlock + pathBlock + diffBlock + output : `<div class="tool-empty">${t('No further details.')}</div>`}${fullButton}${raw}</div>
  </details>`;
}

/** Replace a truncated tool preview with the full host-side output. The
 * session page and the live chat both delegate clicks to this function. */
export async function loadToolOutput(button, sessionId) {
  const callID = button?.dataset.loadOutput;
  if (!callID || !sessionId) return;
  button.disabled = true;
  try {
    // The host slices the output below the WebSocket message limit; the
    // browser reassembles it so very large logs still load in full.
    let offset = 0;
    const parts = [];
    for (;;) {
      const data = await api(`/sessions/${encodeURIComponent(sessionId)}/tool?call=${encodeURIComponent(callID)}&offset=${offset}`);
      parts.push(data.output || '');
      if (!data.next || data.next <= offset) break;
      offset = data.next;
    }
    const text = parts.join('');
    const body = button.closest('.tool-body') || button.parentElement;
    let pre = body?.querySelector('pre.tool-output');
    if (!pre && body) {
      const block = document.createElement('div');
      block.className = 'tool-block';
      block.innerHTML = `<div class="tool-label">${t('Output')}</div><pre class="tool-output"></pre>`;
      body.insertBefore(block, button);
      pre = block.querySelector('pre');
    }
    if (pre) pre.textContent = text;
    button.remove();
  } catch (err) {
    button.disabled = false;
    throw err;
  }
}

export function historyToolCard(described, message, hidden = false) {
  const status = message.kind === 'tool_result' && /(^|\n)(error|failed|exit code [1-9])/i.test(message.tool_output || '') ? 'error' : 'done';
  return toolCardHtml(described, status, hidden);
}

export function reasoningHtml(text, open = transcriptDetail() === 'full') {
  return `<details class="reasoning-card" ${open ? 'open' : ''}><summary>${svgIcon('brain', 14)}<span>${t('Thinking')}</span><span class="reasoning-toggle">${svgIcon('chevronDown', 13)}</span></summary><div class="reasoning-body md">${renderMarkdown(text)}</div></details>`;
}

export function workingHtml() {
  return `<div class="working-indicator" id="working-indicator"><span class="working-dots"><i></i><i></i><i></i></span><span>${t('Agent is working')}</span></div>`;
}
