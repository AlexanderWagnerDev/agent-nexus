import { api } from '../api.js';
import { rememberRecent } from '../palette.js';
import { loadSidebar, setPage } from '../shell.js';
import { draftKey, state, storeGet, storeSet } from '../state.js';
import { openTab } from '../tabs.js';
import { agentGlyph, badge, closeModal, modal, toast } from '../ui.js';
import { wsSend } from '../ws.js';
import { reasoningHtml, renderDiffBlock, renderTranscriptMessages, toolCardHtml, workingHtml } from '../components/transcript.js';
import { startRun } from './session-detail.js';
import { describeTool } from '../chat-format.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { renderMarkdown } from '../markdown.js';
import { agentMeta, cwdKey, esc, fmtRelative, fmtStamp, fmtTime, nativeActivityBadge, projectName, repoLink } from '../ui-format.js';

export async function pageStructuredRun(id, run) {
  const pageGen = state.pageGen;
  let session = null;
  if (run.session_id) {
    session = await api(`/sessions/${encodeURIComponent(run.session_id)}`).catch(() => null);
  }
  const agent = agentMeta(run.agent || session?.agent);
  let versionWarning = '';
  try {
    const { hosts } = await api('/hosts');
    const hostInfo = (hosts || []).find((h) => h.id === run.host_id);
    const info = (hostInfo?.agents || []).find((a) => a.name === (run.agent || session?.agent));
    versionWarning = info?.version_warning || '';
  } catch { /* the banner is optional */ }
  if (pageGen !== state.pageGen) return;
  const title = session?.title || run.session_title || t('Structured chat');
  const cwd = session?.cwd || run.cwd || '';
  const branch = session?.git_branch || run.git_branch || '';
  const host = run.host_name || session?.host_name || run.host_id;
  const contextDefault = !state.embed && storeGet('nexus-context-open', window.innerWidth >= 1180);
  setPage(title, `${projectName(cwd)} · ${agent.label} · ${host}`, `
    <button class="btn small ghost icon-mobile" id="chat-context-toggle" title="${t('Toggle session details')}" aria-label="${t('Toggle session details')}">${svgIcon('panelRight')} <span>${t('Details')}</span></button>
    <button class="btn small danger icon-mobile" id="stop-run" title="${t('Stop this run')}" aria-label="${t('Stop this run')}">${svgIcon('stop')} <span>${t('Stop')}</span></button>
    <a class="btn small ghost icon-mobile" href="#/workspace?pane=${encodeURIComponent(id)}" title="${t('Open split workspace')}" aria-label="${t('Open split workspace')}">${svgIcon('panelRight')} <span>${t('Split')}</span></a>
    <a class="btn small ghost icon-mobile" href="#/sessions" title="${t('All sessions')}" aria-label="${t('All sessions')}">${svgIcon('sessions')} <span>${t('Sessions')}</span></a>`);
  openTab({ id, key: session?.id || id, kind: 'run', href: `#/runs/${id}`, title, agent: run.agent || session?.agent });
  rememberRecent({ id, title, agent: run.agent || session?.agent, hint: `${agent.label} · ${host} · ${projectName(cwd)}`, href: `#/runs/${id}` });
  const view = document.getElementById('view');
  view.className = 'view chat-view';
  view.innerHTML = `
    <div class="chat-page ${contextDefault ? 'context-open' : ''}" id="chat-page">
      <section class="chat-main">
        <div class="chat-statusbar">
          <span class="badge running" id="chat-state">${esc(t((run.state || 'starting').replaceAll('_', ' ')))}</span>
          <span class="chat-agent">${agentGlyph(run.agent || session?.agent)}${esc(agent.label)}</span>
          <span class="chat-sep">·</span>
          <span class="chat-host">${esc(host)}</span>
          ${nativeActivityBadge(session?.active_elsewhere, true)}
          ${branch ? `<span class="branch-chip">${svgIcon('branch', 12)}${esc(branch)}</span>` : ''}
          <button class="icon-btn chat-inline-context" id="chat-inline-context" type="button" title="${t('Toggle session details')}" aria-label="${t('Toggle session details')}">${svgIcon('panelRight')}</button>
          <span class="chat-connection" id="run-note"><span class="connection-dot"></span>Connecting…</span>
        </div>
        <div class="chat-scroll" id="chat">
          <div class="chat-inner">
            <div id="history-top"></div>
            <div id="messages" class="messages"></div>
            <div id="working-slot"></div>
          </div>
        </div>
        <div class="composer-wrap">
          ${versionWarning ? `<div class="notice info small" role="status">${t('Untested agent version')}: ${esc(versionWarning)}</div>` : ''}
          <button class="jump-latest" id="jump-latest" type="button" hidden>${svgIcon('arrowDown', 14)} ${t('Jump to latest')}</button>
          <div id="approval-banner" class="approval-slot"></div>
          <div class="prompt-queue" id="prompt-queue" hidden>
            <div class="prompt-queue-head">
              <strong>${t('Prompt queue')}</strong>
              <span class="composer-pill" id="queue-count">0</span>
              <span class="muted small" id="queue-state"></span>
              <button class="btn ghost small" id="queue-pause" type="button">${t('Pause queue')}</button>
            </div>
            <div class="prompt-queue-schedule">
              <label class="label-inline">${t('Schedule (optional)')}<input class="input" type="datetime-local" id="queue-schedule"></label>
            </div>
            <div class="prompt-queue-items" id="queue-items"></div>
          </div>
          <form class="composer" id="prompt-form">
            <textarea id="prompt-text" rows="1" placeholder="${t('Message {agent}…', { agent: agent.label })}" enterkeyhint="send" autocomplete="off" spellcheck="false"></textarea>
            <div class="composer-config" id="composer-config"></div>
            <div class="attachment-list" id="attachment-list"></div>
            <div class="composer-bar">
              <div class="composer-info">${agentGlyph(run.agent || session?.agent, 'small')}<span>${esc(agent.label)}</span><span class="composer-pill">${esc(run.mode)}</span><span class="composer-pill host-pill">${esc(host)}</span></div>
              <div class="composer-actions">
                <span class="composer-hint">${t('Enter to send · Shift+Enter newline')}</span>
                <input type="file" id="attach-input" multiple hidden>
                <button class="icon-btn" id="attach-button" type="button" title="${t('Attach files')}" aria-label="${t('Attach files')}" hidden>${svgIcon('paperclip')}</button>
                <button class="icon-btn composer-stop" id="interrupt-run" type="button" title="${t('Interrupt current turn')}" aria-label="${t('Interrupt current turn')}" hidden>${svgIcon('stop')}</button>
                <button class="icon-btn" id="queue-prompt" type="button" title="${t('Add to prompt queue')}" aria-label="${t('Add to prompt queue')}">${svgIcon('clock')}</button>
                <button class="send-button" id="send-prompt" type="submit" title="${t('Send message')}" aria-label="${t('Send message')}">${svgIcon('send')}</button>
              </div>
            </div>
          </form>
        </div>
      </section>
      <aside class="chat-context" id="chat-context">
        <div class="context-head"><strong>${t('Session')}</strong><button class="icon-btn" id="context-close" type="button" aria-label="${t('Close details')}">${svgIcon('x')}</button></div>
        <div class="context-body">
          <section class="context-section">
            <div class="aside-label">${t('Run')}</div>
            <div class="meta-list">
              <div><span>${t('State')}</span><strong id="context-state">${esc(t((run.state || 'starting').replaceAll('_', ' ')))}</strong></div>
              <div><span>${t('Agent')}</span><strong>${esc(agent.label)}</strong></div>
              <div><span>${t('Mode')}</span><strong>${esc(run.mode)}</strong></div>
              <div><span>${t('Host')}</span><strong>${esc(host)}</strong></div>
              <div><span>${t('Started')}</span><strong>${esc(fmtTime(run.started_at))}</strong></div>
            </div>
          </section>
          <section class="context-section">
            <div class="aside-label">${t('Native session')}</div>
            ${session ? `<div class="meta-list">
              <div><span>${t('Messages')}</span><strong>${session.message_count || 0}</strong></div>
              <div><span>${t('Updated')}</span><strong>${esc(fmtRelative(session.updated_at))}</strong></div>
            </div>
            <div class="copy-line"><code>${esc(session.native_id || session.id)}</code><button class="icon-btn" data-copy="${esc(session.native_id || session.id)}" title="${t('Copy native id')}">${svgIcon('copy', 14)}</button></div>
            <div class="copy-line"><code>${esc(cwd)}</code><button class="icon-btn" data-copy="${esc(cwd)}" title="${t('Copy working directory')}">${svgIcon('copy', 14)}</button></div>
            ${session.repo_url ? `<div class="aside-repo">${repoLink(session.repo_url)}${session.repo_root && cwdKey(session.repo_root) !== cwdKey(cwd) ? '<span class="muted">worktree</span>' : ''}</div>` : ''}` : `<p class="aside-note">${t('This run was started without a discovered native session.')}</p>`}
          </section>
          <section class="context-section">
            <div class="aside-label">${t('Git')}</div>
            <div id="context-git"><button class="btn small block" id="context-git-load">${t('Load working tree changes')}</button></div>
          </section>
          <section class="context-section">
            <div class="aside-label">${t('Actions')}</div>
            <div class="aside-actions">
              ${session ? `<button class="btn small" id="context-pin">${svgIcon('star')} ${state.favorites.has(session.id) ? t('Unpin session') : t('Pin session')}</button><a class="btn small" href="#/sessions/${esc(session.id)}">${svgIcon('file')} ${t('Read-only transcript')}</a><button class="btn small" id="context-terminal">${svgIcon('terminal')} ${t('Terminal mode')}</button><a class="btn small" href="/api/v1/sessions/${esc(session.id)}/export?format=markdown">${t('Export Markdown')}</a>` : ''}
            </div>
          </section>
          <section class="context-section">
            <div class="aside-label">${t('Continuity')}</div>
            <p class="aside-note">${t("This chat drives the agent's native session. You can return to the same session in the original CLI, and the transcript stays on the host.")}</p>
          </section>
        </div>
      </aside>
      <div class="context-resizer" id="context-resizer" title="${t('Drag to resize details')}"></div>
      <div class="chat-scrim" id="chat-scrim"></div>
    </div>`;

  const chat = document.getElementById('chat');
  const messagesEl = document.getElementById('messages');
  messagesEl.dataset.empty = t('Send a message to continue this native session.');
  const historyTop = document.getElementById('history-top');
  const workingSlot = document.getElementById('working-slot');
  const stateNode = document.getElementById('chat-state');
  const contextState = document.getElementById('context-state');
  const note = document.getElementById('run-note');
  const approval = document.getElementById('approval-banner');
  const form = document.getElementById('prompt-form');
  const textarea = document.getElementById('prompt-text');
  const sendButton = document.getElementById('send-prompt');
  const stopButton = document.getElementById('interrupt-run');
  const jumpButton = document.getElementById('jump-latest');
  const chatPage = document.getElementById('chat-page');
  const contextWidth = Math.min(520, Math.max(240, Number(storeGet('nexus-context-width', 320)) || 320));
  chatPage.style.setProperty('--context-width', `${contextWidth}px`);
  const resizer = document.getElementById('context-resizer');
  let resizing = false;
  resizer.addEventListener('pointerdown', (event) => {
    if (window.innerWidth <= 1100) return;
    resizing = true;
    resizer.setPointerCapture(event.pointerId);
    document.body.style.userSelect = 'none';
    document.body.style.cursor = 'col-resize';
  });
  resizer.addEventListener('pointermove', (event) => {
    if (!resizing) return;
    const width = Math.min(520, Math.max(240, window.innerWidth - event.clientX));
    chatPage.style.setProperty('--context-width', `${width}px`);
  });
  const endResize = () => {
    if (!resizing) return;
    resizing = false;
    document.body.style.userSelect = '';
    document.body.style.cursor = '';
    const width = Number.parseInt(getComputedStyle(chatPage).getPropertyValue('--context-width'), 10);
    if (Number.isFinite(width)) storeSet('nexus-context-width', width);
  };
  resizer.addEventListener('pointerup', endResize);
  resizer.addEventListener('pointercancel', endResize);
  const configBar = document.getElementById('composer-config');
  let runConfigOptions = null;
  const configKey = `nexus-run-config-${id}`;
  const readConfig = () => storeGet(configKey, {});
  const writeConfig = (patch) => storeSet(configKey, { ...readConfig(), ...patch });
  let selectedModelIndex = -1;
  const modelKey = (m) => `${m?.provider || 'agent'}|${m?.id || ''}`;
  const modelList = (key) => {
    const value = storeGet(key, []);
    return Array.isArray(value) ? value.filter((item) => typeof item === 'string') : [];
  };
  const rememberModel = (key) => {
    const recent = modelList('nexus-model-recent').filter((item) => item !== key);
    recent.unshift(key);
    storeSet('nexus-model-recent', recent.slice(0, 8));
  };
  const fuzzyMatch = (query, text) => {
    const q = query.trim().toLowerCase();
    if (!q) return true;
    const haystack = text.toLowerCase();
    if (haystack.includes(q)) return true;
    let index = 0;
    for (const ch of haystack) {
      if (ch === q[index]) index += 1;
      if (index === q.length) return true;
    }
    return index === q.length;
  };
  const selectedModel = () => {
    if (!runConfigOptions?.models?.length || selectedModelIndex < 0) return null;
    return runConfigOptions.models[selectedModelIndex] || null;
  };
  const openModelPicker = () => {
    const models = runConfigOptions?.models || [];
    if (!models.length) return;
    const byKey = new Map(models.map((model, index) => [modelKey(model), { model, index }]));
    let query = '';
    modal(`
      <div class="model-picker-head"><h2>${t('Choose a model')}</h2><span class="muted">${t('{n} available', { n: models.length })}</span></div>
      <label class="model-search">${svgIcon('search', 14)}<input id="model-search" type="search" placeholder="${t('Search model or provider?')}" autocomplete="off" spellcheck="false" autofocus></label>
      <div class="model-picker-list" id="model-picker-list"></div>
      <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Close')}</button></div>`);
    const list = document.getElementById('model-picker-list');
    const search = document.getElementById('model-search');
    const renderList = () => {
      const favorites = new Set(modelList('nexus-model-favorites'));
      const recent = modelList('nexus-model-recent');
      let entries = models.map((model, index) => ({ model, index }));
      if (query.trim()) {
        entries = entries.filter(({ model }) => fuzzyMatch(query, `${model.name || ''} ${model.id || ''} ${model.provider || ''}`));
      } else {
        const rank = (model) => {
          const key = modelKey(model);
          const recentIndex = recent.indexOf(key);
          return recentIndex >= 0 ? recentIndex : (favorites.has(key) ? 100 : 1000);
        };
        entries.sort((a, b) => rank(a.model) - rank(b.model));
      }
      list.innerHTML = entries.length ? entries.map(({ model, index }) => {
        const key = modelKey(model);
        const active = index === selectedModelIndex;
        const variant = (model.variants || []).length ? t('{n} reasoning levels', { n: model.variants.length }) : t('default reasoning');
        return `<div class="model-row ${active ? 'active' : ''}">
          <button class="model-main" type="button" data-model-pick="${esc(key)}" data-model-index="${index}">
            <span class="model-name">${esc(model.name || model.id)}</span>
            <span class="model-meta">${esc(model.provider || 'agent')} &middot; ${esc(variant)}</span>
          </button>
          <button class="model-star ${favorites.has(key) ? 'active' : ''}" type="button" data-model-star="${esc(key)}" aria-label="${favorites.has(key) ? t('Remove favorite') : t('Add favorite')}" aria-pressed="${favorites.has(key) ? 'true' : 'false'}">${svgIcon('star', 14)}</button>
        </div>`;
      }).join('') : `<div class="empty compact">${t('No models match that search.')}</div>`;
    };
    renderList();
    search.addEventListener('input', () => { query = search.value; renderList(); });
    list.addEventListener('click', (event) => {
      const star = event.target.closest('[data-model-star]');
      if (star) {
        const key = star.dataset.modelStar;
        const favorites = modelList('nexus-model-favorites');
        const next = favorites.includes(key) ? favorites.filter((item) => item !== key) : [key, ...favorites];
        storeSet('nexus-model-favorites', next);
        renderList();
        return;
      }
      const pick = event.target.closest('[data-model-pick]');
      if (!pick) return;
      selectedModelIndex = Number(pick.dataset.modelIndex);
      const model = models[selectedModelIndex];
      if (!model) return;
      writeConfig({ model: model.id, provider: model.provider || '', variant: '' });
      rememberModel(modelKey(model));
      closeModal();
      renderComposerConfig();
    });
    requestAnimationFrame(() => search.focus());
  };
  const renderComposerConfig = () => {
    if (!configBar || !runConfigOptions) return;
    const models = runConfigOptions.models || [];
    const modes = runConfigOptions.modes || [];
    if (!models.length && !modes.length) { configBar.innerHTML = ''; configBar.hidden = true; return; }
    const saved = readConfig();
    selectedModelIndex = models.findIndex((m) => m.id === saved.model && (!saved.provider || m.provider === saved.provider));
    if (selectedModelIndex < 0) selectedModelIndex = models.findIndex((m) => m.id === runConfigOptions.current_model && (!runConfigOptions.current_provider || m.provider === runConfigOptions.current_provider));
    if (selectedModelIndex < 0) selectedModelIndex = 0;
    const model = models[selectedModelIndex];
    const variants = model?.variants || [];
    let variant = saved.variant || '';
    if (variant && !variants.includes(variant)) variant = '';
    if (!variant && model?.default_variant && variants.includes(model.default_variant)) variant = model.default_variant;
    if (!variant && model && variants.length && runConfigOptions.current_model === model.id) variant = runConfigOptions.current_variant || '';
    const mode = saved.agent_mode || runConfigOptions.current_mode || '';
    configBar.hidden = false;
    configBar.innerHTML = `
      ${models.length ? `<div class="composer-field"><span>${t('Model')}</span><button class="composer-model" id="opt-model-btn" type="button" title="${t('Switch model')}"><span class="composer-model-name">${esc(model?.name || model?.id || t('Select model'))}</span><span class="composer-model-provider">${esc(model?.provider || '')}</span>${svgIcon('chevronDown', 12)}</button></div>` : ''}
      ${variants.length ? `<label class="composer-field"><span>${t('Reasoning')}</span><select id="opt-variant" class="composer-select"><option value="">${t('Default')}</option>${variants.map((v) => `<option value="${esc(v)}" ${v === variant ? 'selected' : ''}>${esc(v)}</option>`).join('')}</select></label>` : ''}
      ${modes.length ? `<label class="composer-field"><span>${t('Agent')}</span><select id="opt-agent" class="composer-select"><option value="">${t('Default')}</option>${modes.map((m) => `<option value="${esc(m.id)}" ${m.id === mode ? 'selected' : ''}>${esc(m.name || m.id)}</option>`).join('')}</select></label>` : ''}`;
    document.getElementById('opt-model-btn')?.addEventListener('click', openModelPicker);
    document.getElementById('opt-variant')?.addEventListener('change', (event) => writeConfig({ variant: event.target.value }));
    document.getElementById('opt-agent')?.addEventListener('change', (event) => writeConfig({ agent_mode: event.target.value }));
    renderAttachments();
  };
  const currentPromptOptions = () => {
    // Only send a setting the user explicitly changed. Re-sending the current
    // native values on every prompt would rewrite agent configuration the user
    // never touched and can fail on models that do not accept a variant.
    const saved = readConfig();
    const model = selectedModel();
    const variant = document.getElementById('opt-variant')?.value || '';
    const agentMode = document.getElementById('opt-agent')?.value || '';
    const options = {};
    if (model && (saved.model || saved.variant)) {
      options.model = model.id;
      options.provider = model.provider || '';
      if (variant) options.variant = variant;
    }
    if (agentMode && saved.agent_mode) options.agent_mode = agentMode;
    if (pendingAttachments.length) {
      options.attachments = pendingAttachments.map((file) => ({ name: file.name, mime: file.mime, data: file.data }));
    }
    return options;
  };
  let optionsRetries = 0;
  const loadRunOptions = async () => {
    try {
      runConfigOptions = await api(`/runs/${encodeURIComponent(id)}/options`);
      renderComposerConfig();
      const empty = !(runConfigOptions.models?.length || runConfigOptions.modes?.length);
      if (empty && optionsRetries < 8) {
        optionsRetries += 1;
        setTimeout(() => { loadRunOptions().catch(() => {}); }, 1500);
      }
    } catch {
      runConfigOptions = null;
      if (optionsRetries < 8) {
        optionsRetries += 1;
        setTimeout(() => { loadRunOptions().catch(() => {}); }, 1500);
      }
    }
  };
  const attachmentList = document.getElementById('attachment-list');
  const attachButton = document.getElementById('attach-button');
  const attachInput = document.getElementById('attach-input');
  let pendingAttachments = [];
  const attachmentKinds = () => selectedModel()?.inputs || [];
  const allowedAttachmentKinds = () => attachmentKinds().map((kind) => kind.toLowerCase()).filter((kind) => ['image', 'file', 'pdf', 'audio', 'text'].includes(kind));
  const attachmentMatches = (file, kinds) => {
    const type = String(file.type || file.mime || '').toLowerCase();
    return !kinds.length || kinds.includes('file') || kinds.some((kind) => (
      kind === 'pdf' ? type === 'application/pdf' : type.startsWith(kind + '/')
    ));
  };
  const attachmentAllowed = () => allowedAttachmentKinds().length > 0;
  const attachmentAccept = () => {
    const kinds = attachmentKinds().map((kind) => kind.toLowerCase());
    const accepts = [];
    if (kinds.includes('image')) accepts.push('image/*');
    if (kinds.includes('audio')) accepts.push('audio/*');
    if (kinds.includes('pdf')) accepts.push('application/pdf');
    if (kinds.includes('text')) accepts.push('text/*');
    return accepts.join(',') || undefined;
  };
  const renderAttachments = () => {
    if (!attachmentList || !attachButton) return;
    const kinds = allowedAttachmentKinds();
    const allowed = kinds.length > 0;
    attachButton.hidden = !allowed;
    if (attachInput) attachInput.accept = attachmentAccept() || '';
    if (!allowed) pendingAttachments = [];
    else pendingAttachments = pendingAttachments.filter((file) => attachmentMatches(file, kinds));
    attachmentList.innerHTML = pendingAttachments.map((file, index) => `
      <span class="attachment-chip" title="${esc(file.name)}">
        ${file.preview ? `<img src="${file.preview}" alt="">` : svgIcon('file', 13)}
        <span class="attachment-name">${esc(file.name)}</span>
        <small>${Math.max(1, Math.round(file.size / 1024))} KB</small>
        <button class="attachment-remove" type="button" data-remove-attachment="${index}" aria-label="Remove ${esc(file.name)}">${svgIcon('x', 12)}</button>
      </span>`).join('');
    attachmentList.querySelectorAll('[data-remove-attachment]').forEach((button) => button.addEventListener('click', () => {
      pendingAttachments.splice(Number(button.dataset.removeAttachment), 1);
      renderAttachments();
    }));
  };
  attachButton?.addEventListener('click', () => attachInput?.click());
  attachInput?.addEventListener('change', async () => {
    const files = [...(attachInput.files || [])];
    attachInput.value = '';
    const kinds = allowedAttachmentKinds();
    for (const file of files) {
      if (!attachmentMatches(file, kinds)) { toast(`${file.name} is not supported by this model`, true); continue; }
      if (pendingAttachments.length >= 6) { toast('At most 6 attachments are allowed', true); break; }
      if (file.size > 8 * 1024 * 1024) { toast(`${file.name} exceeds the 8 MB limit`, true); continue; }
      const total = pendingAttachments.reduce((sum, item) => sum + item.size, 0) + file.size;
      if (total > 16 * 1024 * 1024) { toast('Attachments exceed the 16 MB total limit', true); break; }
      const dataUrl = await new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result || ''));
        reader.onerror = () => reject(reader.error || new Error('Could not read file'));
        reader.readAsDataURL(file);
      }).catch((err) => { toast(err.message, true); return ''; });
      if (!dataUrl) continue;
      pendingAttachments.push({
        name: file.name, mime: file.type || 'application/octet-stream',
        data: dataUrl.split(',')[1] || '', size: file.size,
        preview: String(file.type || '').startsWith('image/') ? dataUrl : '',
      });
    }
    renderAttachments();
  });

  const assistantRecords = new Map();
  const toolRecords = new Map();
  const reasoningRecords = new Map();
  const completedMessages = new Set();
  const initialCursor = run.last_event_seq || 0;
  let lastSeq = initialCursor;
  let subscribedGen = 0;
  let runEpoch = '';
  let historyReady = false;
  const pendingLiveEvents = [];
  const historyAssistantTexts = new Set();
  let historyOffset = 0;
  let historyTotal = 0;
  let autoScroll = true;
  let currentState = run.state || 'starting';

  const setContext = (open) => {
    chatPage.classList.toggle('context-open', open);
    storeSet('nexus-context-open', open);
  };
  document.getElementById('chat-context-toggle').addEventListener('click', () => setContext(!chatPage.classList.contains('context-open')));
  document.getElementById('chat-inline-context').addEventListener('click', () => setContext(!chatPage.classList.contains('context-open')));
  document.getElementById('context-close').addEventListener('click', () => setContext(false));
  document.getElementById('chat-scrim').addEventListener('click', () => setContext(false));

  const isNearBottom = () => chat.scrollHeight - chat.scrollTop - chat.clientHeight < 90;
  const scrollBottom = (force = false) => {
    if (force || autoScroll) {
      chat.scrollTop = chat.scrollHeight;
      jumpButton.hidden = true;
    } else {
      jumpButton.hidden = false;
    }
  };
  chat.addEventListener('scroll', () => {
    autoScroll = isNearBottom();
    jumpButton.hidden = autoScroll;
  });
  jumpButton.addEventListener('click', () => {
    autoScroll = true;
    scrollBottom(true);
  });

  let gitRefreshHook = null;
  const setState = (state) => {
    currentState = state || 'unknown';
    run.state = state;
    const label = (state || 'unknown').replaceAll('_', ' ');
    stateNode.textContent = t(label);
    stateNode.className = 'badge ' + (state || 'unknown');
    if (contextState) contextState.textContent = t(label);
    const busy = state === 'running' || state === 'starting';
    stopButton.hidden = !busy;
    workingSlot.innerHTML = busy ? workingHtml() : '';
    const topStop = document.getElementById('stop-run');
    topStop.hidden = !busy;
    topStop.disabled = !busy;
    if (!busy && gitRefreshHook) gitRefreshHook();
    scrollBottom();
  };
  setState(run.state || 'starting');

  const appendUser = (text) => {
    const article = document.createElement('article');
    article.className = 'turn turn-user';
    article.innerHTML = `<div class="turn-meta"><span>You</span><time>${esc(fmtStamp(new Date()))}</time></div><div class="turn-body"></div>`;
    article.querySelector('.turn-body').textContent = text;
    messagesEl.appendChild(article);
    scrollBottom();
  };

  const assistantRecord = (messageID) => {
    const key = messageID || 'assistant';
    if (assistantRecords.has(key)) return assistantRecords.get(key);
    const article = document.createElement('article');
    article.className = 'turn turn-assistant';
    article.innerHTML = `<div class="turn-meta">${agentGlyph(run.agent || session?.agent, 'small')}<span>${esc(agent.label)}</span><time>${esc(fmtStamp(new Date()))}</time></div><div class="turn-body md"></div>`;
    messagesEl.appendChild(article);
    const rec = { node: article, body: article.querySelector('.turn-body'), raw: '', timer: null, rendered: '' };
    assistantRecords.set(key, rec);
    return rec;
  };

  const renderAssistant = (rec, immediate = false) => {
    const paint = () => {
      if (rec.rendered === rec.raw) return;
      rec.rendered = rec.raw;
      rec.body.innerHTML = renderMarkdown(rec.raw);
      scrollBottom();
    };
    if (immediate) {
      clearTimeout(rec.timer);
      paint();
      return;
    }
    if (rec.timer) return;
    rec.timer = setTimeout(() => {
      rec.timer = null;
      paint();
    }, 70);
  };

  const appendAssistantText = (messageID, text, replace = false) => {
    const rec = assistantRecord(messageID);
    if (replace && text) rec.raw = text;
    else if (text) rec.raw += text;
    renderAssistant(rec, replace);
  };

  const updateTool = (callID, name, input, output, status) => {
    const key = callID || ('tool-' + toolRecords.size);
    let rec = toolRecords.get(key);
    if (!rec) {
      rec = { name: name || 'tool', input: input || '', output: '', status: 'running', node: null };
      toolRecords.set(key, rec);
    }
    if (name) rec.name = name;
    if (input !== undefined) rec.input = input || '';
    if (output !== undefined) rec.output = output || '';
    if (status) rec.status = status;
    const described = describeTool(rec.name, rec.input, rec.output);
    const wrapper = document.createElement('div');
    wrapper.innerHTML = toolCardHtml(described, rec.status);
    const node = wrapper.firstElementChild;
    if (rec.node) {
      node.open = rec.node.open || rec.status === 'error';
      rec.node.replaceWith(node);
    } else {
      messagesEl.appendChild(node);
    }
    rec.node = node;
    scrollBottom();
  };

  const updateReasoning = (messageID, text) => {
    if (!text) return;
    const key = messageID || 'reasoning';
    let rec = reasoningRecords.get(key);
    if (!rec) {
      const wrapper = document.createElement('div');
      wrapper.innerHTML = reasoningHtml(text);
      const node = wrapper.firstElementChild;
      messagesEl.appendChild(node);
      rec = { node, raw: text, timer: null };
      reasoningRecords.set(key, rec);
    } else {
      rec.raw += text;
      if (rec.timer) return;
      rec.timer = setTimeout(() => {
        rec.timer = null;
        rec.node.querySelector('.reasoning-body').innerHTML = renderMarkdown(rec.raw);
        scrollBottom();
      }, 80);
    }
  };

  const showApproval = (event) => {
    const resources = (event.resources || []).join('\n');
    const card = document.createElement('div');
    card.className = 'approval-card';
    card.innerHTML = `
      <div class="approval-head">${svgIcon('alert', 16)}<strong>${t('Approval required')}</strong><span class="badge waiting_for_input">${esc(event.action || 'permission')}</span></div>
      ${resources || event.text ? `<pre>${esc(resources || event.text)}</pre>` : ''}
      <div class="approval-actions"><button class="btn small primary" data-approval="once">${t('Allow once')}</button><button class="btn small danger" data-approval="reject">${t('Deny')}</button></div>`;
    card.querySelectorAll('[data-approval]').forEach((button) => button.addEventListener('click', () => {
      wsSend({ type: 'approval', run_id: id, approval_id: event.approval_id, decision: button.dataset.approval });
      card.remove();
      noteText(t('Approval sent'));
    }));
    approval.appendChild(card);
    autoScroll = true;
    scrollBottom(true);
    noteText('Waiting for your approval');
    setState('waiting_for_input');
  };

  const noteText = (text, live = false) => {
    note.innerHTML = `<span class="connection-dot ${live ? 'live' : ''}"></span>${esc(text)}`;
  };

  const appendError = (text) => {
    const node = document.createElement('div');
    node.className = 'chat-error';
    node.innerHTML = `${svgIcon('alert', 15)}<div><strong>Run error</strong><pre>${esc(text)}</pre></div>`;
    messagesEl.appendChild(node);
    scrollBottom(true);
  };

  const handleEvent = (event) => {
    if (!event || (event.seq && event.seq <= lastSeq)) return;
    if (!historyReady) {
      pendingLiveEvents.push(event);
      return;
    }
    if (event.seq) lastSeq = Math.max(lastSeq, event.seq);
    switch (event.type) {
      case 'message.started':
        assistantRecord(event.message_id);
        break;
      case 'message.delta':
        appendAssistantText(event.message_id, event.text || '');
        break;
      case 'message.completed': {
        const key = event.message_id || 'assistant';
        if (completedMessages.has(key)) break;
        completedMessages.add(key);
        appendAssistantText(event.message_id, event.text || '', true);
        renderAssistant(assistantRecord(event.message_id), true);
        break;
      }
      case 'reasoning':
        updateReasoning(event.message_id, event.text || '');
        break;
      case 'tool.started':
        updateTool(event.call_id, event.tool_name, event.tool_input, undefined, 'running');
        break;
      case 'tool.input':
        updateTool(event.call_id, undefined, event.tool_input, undefined, 'running');
        break;
      case 'tool.completed':
        updateTool(event.call_id, undefined, undefined, event.error ? `error: ${event.error}` : (event.tool_output || ''), event.error ? 'error' : 'done');
        break;
      case 'approval.requested':
        showApproval(event);
        break;
      case 'approval.resolved':
        approval.innerHTML = '';
        noteText(`Approval ${event.decision || 'resolved'}`, true);
        break;
      case 'run.state':
        setState(event.state);
        noteText(event.state === 'idle' ? 'Idle — ready for your next message' : `Agent ${event.state.replaceAll('_', ' ')}`, event.state === 'running');
        break;
      case 'run.error':
        setState('failed');
        noteText(event.error || 'run failed');
        appendError(event.error || 'run failed');
        break;
    }
  };

  const flushPendingLiveEvents = () => {
    pendingLiveEvents.sort((a, b) => (a.seq || 0) - (b.seq || 0));
    for (const event of pendingLiveEvents.splice(0)) {
      if (event.type === 'message.completed') {
        const text = String(event.text || '').trim();
        if (text && historyAssistantTexts.has(text)) {
          historyAssistantTexts.delete(text);
          const key = event.message_id || 'assistant';
          const rec = assistantRecords.get(key);
          if (rec?.node) {
            rec.node.remove();
            assistantRecords.delete(key);
          }
          if (event.seq) lastSeq = Math.max(lastSeq, event.seq);
          continue;
        }
      }
      handleEvent(event);
    }
  };

  const handler = (msg) => {
    if (msg.run_id !== id && msg.type !== 'ready') return;
    if (msg.type === 'queue.changed') loadQueue().catch(() => {});
    if (msg.type === 'run.prompt.sent' && msg.message) appendUser(msg.message);
    if (msg.type === 'run.event' && msg.event) handleEvent(msg.event);
    if (msg.type === 'run.state') {
      const epochChanged = Boolean(msg.epoch && runEpoch && msg.epoch !== runEpoch);
      if (msg.epoch) runEpoch = msg.epoch;
      if (epochChanged) {
        // The hub restarted and its per-run sequence starts from zero again.
        lastSeq = 0;
        sendSubscribe();
      }
      setState(msg.state);
      noteText(msg.state === 'idle' ? 'Idle — ready for your next message' : `Agent ${msg.state.replaceAll('_', ' ')}`, msg.state === 'running');
    }
    if (msg.type === 'run.exit') {
      setState(msg.state || 'exited');
      noteText(msg.message || 'Run exited');
    }
    if (msg.type === 'error') toast(msg.error || 'error', true);
  };
  state.wsHandlers.add(handler);

  const sendSubscribe = () => {
    const last = Math.max(lastSeq, initialCursor);
    if (wsSend({ type: 'subscribe', run_id: id, last_seq: last })) subscribedGen = state.wsGen;
  };

  const loadEarlier = async () => {
    if (!historyOffset) return;
    const before = chat.scrollHeight;
    const top = chat.scrollTop;
    const start = Math.max(0, historyOffset - 120);
    const tr = await api(`/sessions/${encodeURIComponent(run.session_id)}/transcript?offset=${start}&limit=${historyOffset - start}`);
    if (tr.messages?.length) {
      messagesEl.insertAdjacentHTML('afterbegin', renderTranscriptMessages(tr.messages, run.agent || session?.agent));
      chat.scrollTop = top + (chat.scrollHeight - before);
    }
    historyOffset = start;
    historyTop.innerHTML = historyOffset > 0
      ? `<button class="btn small ghost history-button" id="load-earlier">${t('Load earlier messages ({n} remaining)', { n: historyOffset.toLocaleString() })}</button>`
      : '';
    document.getElementById('load-earlier')?.addEventListener('click', () => loadEarlier().catch((err) => toast(err.message, true)));
  };

  const loadHistory = async () => {
    if (!run.session_id) return;
    try {
      const head = await api(`/sessions/${encodeURIComponent(run.session_id)}/transcript?offset=0&limit=1`);
      historyTotal = head.total || 0;
      if (!historyTotal) return;
      const limit = Math.min(200, historyTotal);
      const offset = Math.max(0, historyTotal - limit);
      const tr = historyTotal > 1 ? await api(`/sessions/${encodeURIComponent(run.session_id)}/transcript?offset=${offset}&limit=${limit}`) : head;
      historyOffset = offset;
      const visible = (tr.messages || []).filter((m) => m.kind === 'tool_call' || m.kind === 'tool_result' || m.role !== 'user' || !/^\s*<(environment_context|recommended_plugins|user_instructions|permissions|skills)/i.test(m.text || ''));
      for (const message of visible) {
        if (message.role === 'assistant' && (!message.kind || message.kind === 'text')) {
          const text = String(message.text || '').trim();
          if (text) historyAssistantTexts.add(text);
        }
      }
      messagesEl.innerHTML = renderTranscriptMessages(visible, run.agent || session?.agent);
      historyTop.innerHTML = offset > 0
        ? `<button class="btn small ghost history-button" id="load-earlier">${t('Load earlier messages ({n} remaining)', { n: offset.toLocaleString() })}</button>`
        : '';
      document.getElementById('load-earlier')?.addEventListener('click', () => loadEarlier().catch((err) => toast(err.message, true)));
      autoScroll = true;
      scrollBottom(true);
    } catch { /* history is optional */ }
    historyReady = true;
    flushPendingLiveEvents();
  };

  api(`/runs/${id}/lease`, { method: 'POST' }).then(() => {
    noteText(t('You have control of this run'), true);
  }).catch((err) => {
    noteText(t('Read-only:') + ' ' + err.message);
    textarea.disabled = true;
    sendButton.disabled = true;
  });

  document.getElementById('stop-run').addEventListener('click', async () => {
    try { await api(`/runs/${id}/stop`, { method: 'POST' }); toast(t('Stopping run')); }
    catch (err) { toast(err.message, true); }
  });
  stopButton.addEventListener('click', () => {
    wsSend({ type: 'interrupt', run_id: id });
    noteText(t('Interrupt requested'));
  });

  const draft = storeGet(draftKey(id), '');
  if (draft) textarea.value = draft;
  const autosize = () => {
    textarea.style.height = 'auto';
    textarea.style.height = `${Math.min(220, textarea.scrollHeight)}px`;
  };
  autosize();
  textarea.addEventListener('input', () => {
    autosize();
    storeSet(draftKey(id), textarea.value);
  });
  const coarsePointer = window.matchMedia('(pointer: coarse)').matches;
  textarea.addEventListener('keydown', (event) => {
    if (event.isComposing) return;
    if (event.key === 'Enter' && !event.shiftKey && (!coarsePointer || event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      form.requestSubmit();
    }
  });
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    const text = textarea.value.trim();
    if (!text) return;
    const promptOptions = currentPromptOptions();
    const message = { type: 'prompt', run_id: id, text };
    if (Object.keys(promptOptions).length) message.options = promptOptions;
    if (!wsSend(message)) {
      toast(t('WebSocket is reconnecting; try again in a moment'), true);
      return;
    }
    appendUser(text);
    textarea.value = '';
    pendingAttachments = [];
    renderAttachments();
    storeSet(draftKey(id), '');
    autosize();
    autoScroll = true;
    setState('running');
    noteText(t('Agent is working'), true);
    scrollBottom(true);
  });

  // ---- prompt queue ----
  const queuePanel = document.getElementById('prompt-queue');
  const queueItemsEl = document.getElementById('queue-items');
  const queueCount = document.getElementById('queue-count');
  const queueState = document.getElementById('queue-state');
  const queuePause = document.getElementById('queue-pause');
  const queueSchedule = document.getElementById('queue-schedule');
  let queueItems = [];
  let queuePaused = false;
  let queueBusy = false;

  const queueScheduleISO = (value) => {
    if (!value) return '';
    const date = new Date(value);
    return Number.isFinite(date.getTime()) ? date.toISOString() : '';
  };
  const renderQueue = () => {
    const pending = queueItems.filter((item) => item.state !== 'sent');
    queuePanel.hidden = pending.length === 0 && !queuePaused;
    queueCount.textContent = String(pending.length);
    queueState.textContent = queuePaused ? t('paused') : '';
    queuePause.textContent = queuePaused ? t('Resume queue') : t('Pause queue');
    if (!pending.length) {
      queueItemsEl.innerHTML = `<div class="muted small">${t('Queue is empty. Use the clock button to add the current draft.')}</div>`;
      return;
    }
    queueItemsEl.innerHTML = pending.map((item, index) => `
      <div class="prompt-queue-item" data-id="${esc(item.id)}">
        <div class="prompt-queue-text">${esc(item.text)}</div>
        <div class="prompt-queue-meta">
          <span class="badge ${item.state === 'failed' ? 'failed' : 'idle'}">${esc(t(item.state))}</span>
          ${item.scheduled_at ? `<span>${esc(fmtStamp(item.scheduled_at))}</span>` : ''}
          <span>${esc(t('position'))} ${index + 1}</span>
        </div>
        ${item.last_error ? `<div class="notice error small">${esc(item.last_error)}</div>` : ''}
        <div class="prompt-queue-actions">
          <button class="icon-btn" type="button" data-queue-action="up" title="${t('Move up')}" aria-label="${t('Move up')}" ${index === 0 ? 'disabled' : ''}>↑</button>
          <button class="icon-btn" type="button" data-queue-action="down" title="${t('Move down')}" aria-label="${t('Move down')}" ${index === pending.length - 1 ? 'disabled' : ''}>↓</button>
          <button class="btn ghost small" type="button" data-queue-action="edit">${t('Edit')}</button>
          <button class="btn ghost small" type="button" data-queue-action="remove">${t('Remove')}</button>
        </div>
      </div>`).join('');
  };
  const loadQueue = async () => {
    const data = await api(`/runs/${encodeURIComponent(id)}/queue`);
    queueItems = data.items || [];
    queuePaused = Boolean(data.paused);
    renderQueue();
  };
  const reorderQueue = async (idToMove, direction) => {
    if (queueBusy) return;
    const ids = queueItems.filter((item) => item.state === 'queued').map((item) => item.id);
    const index = ids.indexOf(idToMove);
    const target = index + direction;
    if (index < 0 || target < 0 || target >= ids.length) return;
    [ids[index], ids[target]] = [ids[target], ids[index]];
    queueBusy = true;
    try {
      await api(`/runs/${encodeURIComponent(id)}/queue/order`, { method: 'PUT', body: { ids } });
      await loadQueue();
    } catch (err) { toast(err.message, true); } finally { queueBusy = false; }
  };
  const editQueueItem = (item) => {
    const scheduled = item.scheduled_at ? new Date(item.scheduled_at).toISOString().slice(0, 16) : '';
    modal(`
      <h2>${t('Edit queued prompt')}</h2>
      <label class="label">${t('Prompt')}<textarea class="input" id="queue-edit-text" rows="5">${esc(item.text)}</textarea></label>
      <label class="label">${t('Schedule (optional)')}<input class="input" type="datetime-local" id="queue-edit-schedule" value="${esc(scheduled)}"></label>
      <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Cancel')}</button><button class="btn primary" type="button" id="queue-edit-save">${t('Save')}</button></div>`);
    document.getElementById('queue-edit-save').addEventListener('click', async () => {
      const text = document.getElementById('queue-edit-text').value.trim();
      if (!text) return;
      try {
        await api(`/runs/${encodeURIComponent(id)}/queue/${encodeURIComponent(item.id)}`, {
          method: 'PATCH',
          body: { text, scheduled_at: queueScheduleISO(document.getElementById('queue-edit-schedule').value) },
        });
        closeModal();
        await loadQueue();
        toast(t('Queue updated'));
      } catch (err) { toast(err.message, true); }
    });
  };
  queueItemsEl.addEventListener('click', async (event) => {
    const button = event.target.closest('[data-queue-action]');
    const row = event.target.closest('[data-id]');
    if (!button || !row) return;
    const item = queueItems.find((entry) => entry.id === row.dataset.id);
    if (!item) return;
    const action = button.dataset.queueAction;
    if (action === 'up') return reorderQueue(item.id, -1);
    if (action === 'down') return reorderQueue(item.id, 1);
    if (action === 'edit') return editQueueItem(item);
    if (action === 'remove') {
      try {
        await api(`/runs/${encodeURIComponent(id)}/queue/${encodeURIComponent(item.id)}`, { method: 'DELETE' });
        await loadQueue();
        toast(t('Removed from queue'));
      } catch (err) { toast(err.message, true); }
    }
  });
  queuePause.addEventListener('click', async () => {
    try {
      await api(`/runs/${encodeURIComponent(id)}/queue/pause`, { method: 'POST', body: { paused: !queuePaused } });
      await loadQueue();
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('queue-prompt').addEventListener('click', async () => {
    const text = textarea.value.trim();
    if (!text) { toast(t('Write a prompt first'), true); return; }
    const options = currentPromptOptions();
    const body = { text };
    if (Object.keys(options).length) body.options = options;
    const scheduled = queueScheduleISO(queueSchedule.value);
    if (scheduled) body.scheduled_at = scheduled;
    try {
      await api(`/runs/${encodeURIComponent(id)}/queue`, { method: 'POST', body });
      textarea.value = '';
      pendingAttachments = [];
      renderAttachments();
      storeSet(draftKey(id), '');
      autosize();
      queueSchedule.value = '';
      await loadQueue();
      toast(t('Added to prompt queue'));
    } catch (err) { toast(err.message, true); }
  });
  loadQueue().catch(() => {});

  if (session) {
    document.getElementById('context-pin').addEventListener('click', async () => {
      const on = !state.favorites.has(session.id);
      try {
        await api(`/sessions/${session.id}/favorite`, { method: on ? 'PUT' : 'DELETE' });
        if (on) state.favorites.add(session.id); else state.favorites.delete(session.id);
        document.getElementById('context-pin').innerHTML = `${svgIcon('star')} ${on ? 'Unpin session' : 'Pin session'}`;
        loadSidebar().catch(() => {});
      } catch (err) { toast(err.message, true); }
    });
    document.getElementById('context-terminal').addEventListener('click', () => startRun(session, false, 'terminal'));
  }
  const loadGit = async (silent = false) => {
    if (!session) return;
    const box = document.getElementById('context-git');
    if (!box) return;
    if (!silent) box.innerHTML = '<div class="empty compact"><span class="spinner"></span></div>';
    try {
      const g = await api(`/sessions/${session.id}/git`);
      const signature = [g.branch, g.root, g.status, g.stat, g.diff, g.file_count, g.diff_truncated].join('\u0000');
      if (silent && signature === lastGitSignature) return;
      lastGitSignature = signature;
      const truncated = g.status_truncated || g.stat_truncated || g.diff_truncated;
      box.innerHTML = `
        <div class="git-context-head">${g.branch ? `<span class="branch-chip">${svgIcon('branch', 12)}${esc(g.branch)}</span>` : ''}<span class="muted small">${esc(fmtTime(new Date().toISOString()))}</span><button class="btn ghost small" id="git-refresh" title="Refresh changes">${svgIcon('refresh', 13)}</button></div>
        ${g.root ? `<div class="copy-line"><code>${esc(g.root)}</code></div>` : ''}
        ${g.file_count ? `<div class="muted small">${esc(countLabel(g.file_count, 'changed file', 'changed files'))}</div>` : ''}
        <pre class="git-status">${esc(g.status?.trim() || t('Clean working tree'))}</pre>
        ${g.stat ? `<pre class="git-stat-small">${esc(g.stat)}</pre>` : ''}
        ${truncated ? `<p class="hint">${t('The host truncated this output. Open the changes view for the complete diff.')}</p>` : ''}
        ${g.diff ? `<details class="git-diff" open><summary>${t('Uncommitted changes')}</summary>${renderDiffBlock(g.diff)}</details>` : ''}`;
    } catch (err) {
      // A background poll must not erase a readable diff because the host was
      // briefly busy or offline. Manual refreshes still show the error.
      if (silent) return;
      box.innerHTML = `<div class="notice error small">${esc(err.message)}</div><button class="btn small block mt-1" id="context-git-load">Try again</button>`;
    }
  };
  let lastGitSignature = '';
  gitRefreshHook = () => loadGit(true).catch(() => {});
  document.getElementById('context-git').addEventListener('click', (event) => {
    if (event.target.closest('#context-git-load') || event.target.closest('#git-refresh')) loadGit();
  });
  // Live diff while a run is active: the host re-reads the working tree; the
  // hub never stores the diff. Refresh only while the details panel is open.
  let gitTimer = null;
  const scheduleGitRefresh = () => {
    clearInterval(gitTimer);
    if (!session) return;
    gitTimer = setInterval(() => {
      if (!document.getElementById('context-git')) return;
      if (!chatPage.classList.contains('context-open')) return;
      if (currentState !== 'running' && currentState !== 'waiting_for_input' && currentState !== 'starting') return;
      loadGit(true).catch(() => {});
    }, 5000);
  };
  scheduleGitRefresh();

  sendSubscribe();
  await loadHistory().catch(() => {});
  loadRunOptions().catch(() => {});
  const timer = setInterval(() => {
    if (!state.me) { clearInterval(timer); return; }
    if (state.ws?.readyState === WebSocket.OPEN) {
      if (subscribedGen !== state.wsGen) sendSubscribe();
      if (note.textContent.includes('Connecting')) noteText('Connected. Send a message.');
    } else {
      noteText('Reconnecting…');
    }
  }, 800);

  state.pageCleanup = () => {
    clearInterval(timer);
    clearInterval(gitTimer);
    gitRefreshHook = null;
    state.wsHandlers.delete(handler);
  };
  textarea.focus();
}
