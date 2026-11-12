import { api } from '../api.js';
import { ensureStepUp } from '../auth.js';
import { rememberRecent } from '../palette.js';
import { loadSidebar, renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { openTab } from '../tabs.js';
import { agentGlyph, closeModal, modal, stateBadge, toast } from '../ui.js';
import { renderDiffBlock, renderTranscriptMessages } from '../components/transcript.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { agentMeta, cwdKey, esc, fmtRelative, fmtTime, nativeActivityBadge, projectName, repoLink } from '../ui-format.js';

export async function pageSession(id) {
  renderShell('sessions');
  const view = setPage(t('Session'), t('Loading transcript…'), `<a class="btn small" href="#/sessions">${t('Back')}</a>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const s = await api(`/sessions/${id}`);
  const agent = agentMeta(s.agent);
  let tags = Array.isArray(s.tags) ? [...s.tags] : [];
  let versionWarning = '';
  let structuredAllowed = s.agent === 'opencode' || s.agent === 'codex' || s.agent === 'claude';
  try {
    const { hosts } = await api('/hosts');
    const host = (hosts || []).find((h) => h.id === s.host_id);
    const info = (host?.agents || []).find((a) => a.name === s.agent);
    versionWarning = info?.version_warning || '';
    structuredAllowed = Boolean(info?.capabilities?.includes('structured'));
  } catch { /* keep the previous capability assumption */ }
  openTab({ id: s.id, key: s.id, kind: 'session', href: `#/sessions/${s.id}`, title: s.local_title || s.title || 'Untitled session', agent: s.agent });
  rememberRecent({ id: s.id, title: s.local_title || s.title || 'Untitled session', agent: s.agent, hint: `${agent.label} · ${s.host_name} · ${projectName(s.cwd)}`, href: `#/sessions/${s.id}` });
  const title = s.local_title || s.title || 'Session';
  setPage(title, `${projectName(s.cwd)} · ${agent.label} · ${s.host_name}`, `
    <button class="btn small" id="favorite-session">${svgIcon('star')} ${state.favorites.has(s.id) ? t('Pinned') : t('Pin')}</button>
    <button class="btn small" id="resume-session">${svgIcon('terminal')} ${t('Terminal')}</button>
    ${structuredAllowed ? `<button class="btn small primary" id="structured-session">${svgIcon('bot')} ${t('Open chat')}</button>` : ''}
    <a class="btn small ghost" href="#/sessions">${t('Back')}</a>`);
  const favorite = async (on) => {
    await api(`/sessions/${s.id}/favorite`, { method: on ? 'PUT' : 'DELETE' });
    if (on) state.favorites.add(s.id); else state.favorites.delete(s.id);
    const button = document.getElementById('favorite-session');
    if (button) button.innerHTML = `${svgIcon('star')} ${on ? t('Pinned') : t('Pin')}`;
    loadSidebar().catch(() => {});
  };
  document.getElementById('favorite-session').addEventListener('click', () => favorite(!state.favorites.has(s.id)).catch((e) => toast(e.message, true)));
  document.getElementById('resume-session').addEventListener('click', () => startRun(s, false, 'terminal'));
  document.getElementById('structured-session')?.addEventListener('click', () => startRun(s, false, 'structured'));
  view.innerHTML = `
    <div class="detail-layout">
      <section class="detail-main">
        <div class="session-banner">
          ${agentGlyph(s.agent)}
          <div class="session-banner-main">
            <strong>${esc(title)} <button class="icon-btn" id="edit-local-title" type="button" title="${t('Local title')}" aria-label="${t('Local title')}">${svgIcon('file', 13)}</button></strong>
            <span class="session-banner-path">${esc(s.cwd || 'unknown directory')}</span>
          </div>
          ${stateBadge(s.state)}
          ${nativeActivityBadge(s.active_elsewhere)}
          <span class="session-banner-meta">${s.message_count || 0} ${t((s.message_count || 0) === 1 ? 'message' : 'messages')} · ${t('updated')} ${esc(fmtRelative(s.updated_at))}</span>
        </div>
        ${s.active_elsewhere ? `<div class="notice info mt-1">${t('An agent process appears to have this session open in a native terminal. Opening the chat can conflict with that writer; the hub asks before forcing a resume.')}</div>` : ''}
        ${s.warning ? `<div class="notice mt-1">${esc(s.warning)}</div>` : ''}
        ${versionWarning ? `<div class="notice info mt-1">${t('Untested agent version')}: ${esc(versionWarning)}</div>` : ''}
        <div id="transcript" class="transcript"><div class="empty"><span class="spinner"></span></div></div>
        <div class="load-more" id="load-more-wrap"></div>
      </section>
      <aside class="detail-aside">
        <div class="aside-section">
          <div class="aside-label">${t('Session')}</div>
          <div class="meta-list">
            <div><span>${t('Agent')}</span><strong>${esc(agent.label)}</strong></div>
            <div><span>${t('Host')}</span><strong>${esc(s.host_name)}</strong></div>
            <div><span>${t('Created')}</span><strong>${esc(fmtTime(s.created_at))}</strong></div>
            <div><span>${t('Updated')}</span><strong>${esc(fmtTime(s.updated_at))}</strong></div>
          </div>
        </div>
        <div class="aside-section">
          <div class="aside-label">${t('Local metadata')}</div>
          <p class="aside-note">${t('Local metadata only. Native titles and transcripts are never changed.')}</p>
          <label class="label">${t('Local title')}<input class="input" id="local-title-input" maxlength="200" value="${esc(s.local_title || '')}" placeholder="${esc(s.title || t('Untitled session'))}"></label>
          <button class="btn small mt-1" id="save-local-title" type="button">${t('Save')}</button>
          <div class="aside-label mt-2">${t('Tags')}</div>
          <div class="tag-list" id="tag-list">${tags.map((tag) => `<span class="tag-chip">${esc(tag)}<button class="icon-btn" type="button" data-remove-tag="${esc(tag)}" aria-label="${t('Remove tag')}">${svgIcon('x', 11)}</button></span>`).join('') || `<span class="muted small">${t('No tags yet')}</span>`}</div>
          <div class="split mt-1">
            <input class="input" id="tag-input" maxlength="32" placeholder="${t('Add tag')}">
            <button class="btn small" id="add-tag" type="button">${t('Add tag')}</button>
          </div>
        </div>
        <div class="aside-section">
          <div class="aside-label">${t('Native identity')}</div>
          <div class="copy-line"><code>${esc(s.native_id || s.id)}</code><button class="icon-btn" data-copy="${esc(s.native_id || s.id)}" title="${t('Copy native id')}">${svgIcon('copy', 14)}</button></div>
          <div class="copy-line"><code>${esc(s.cwd || '')}</code><button class="icon-btn" data-copy="${esc(s.cwd || '')}" title="${t('Copy working directory')}">${svgIcon('copy', 14)}</button></div>
          ${s.git_branch ? `<div class="branch-chip wide">${svgIcon('branch', 13)}${esc(s.git_branch)}</div>` : ''}
          ${s.repo_url ? `<div class="aside-repo">${repoLink(s.repo_url)}${s.repo_root && cwdKey(s.repo_root) !== cwdKey(s.cwd) ? `<span class="muted">${t('worktree')}</span>` : ''}</div>` : ''}
        </div>
        <div class="aside-section">
          <div class="aside-label">${t('Actions')}</div>
          <div class="aside-actions">
            <a class="btn small" id="git-session" href="#/sessions/${s.id}/changes">${svgIcon('git')} ${t('Review changes')}</a>
            ${s.agent === 'codex' || s.agent === 'claude' ? `<button class="btn small" id="fork-session">${t('Fork session')}</button>` : ''}
            <a class="btn small" href="/api/v1/sessions/${encodeURIComponent(s.id)}/export?format=markdown">${svgIcon('file')} ${t('Export Markdown')}</a>
            <a class="btn small" href="/api/v1/sessions/${encodeURIComponent(s.id)}/export?format=json">${t('Export JSON')}</a>
          </div>
        </div>
        <div class="aside-section">
          <div class="aside-label">${t('Native continuity')}</div>
          <p class="aside-note">${t("Continuing this conversation reopens the agent's own native session. The original CLI can still resume it afterwards.")}</p>
        </div>
      </aside>
    </div>`;
  document.getElementById('fork-session')?.addEventListener('click', () => startRun(s, true, 'terminal'));
  const renderTagList = () => {
    const box = document.getElementById('tag-list');
    if (!box) return;
    box.innerHTML = tags.map((tag) => `<span class="tag-chip">${esc(tag)}<button class="icon-btn" type="button" data-remove-tag="${esc(tag)}" aria-label="${t('Remove tag')}">${svgIcon('x', 11)}</button></span>`).join('') || `<span class="muted small">${t('No tags yet')}</span>`;
    box.querySelectorAll('[data-remove-tag]').forEach((button) => button.addEventListener('click', async () => {
      try {
        const result = await api(`/sessions/${encodeURIComponent(s.id)}/tags/${encodeURIComponent(button.dataset.removeTag)}`, { method: 'DELETE' });
        tags = result.tags || [];
        renderTagList();
        toast(t('Tag removed'));
      } catch (err) { toast(err.message, true); }
    }));
  };
  renderTagList();
  document.getElementById('save-local-title').addEventListener('click', async () => {
    try {
      const result = await api(`/sessions/${encodeURIComponent(s.id)}`, { method: 'PATCH', body: { local_title: document.getElementById('local-title-input').value.trim() } });
      s.local_title = result.local_title || '';
      const shown = s.local_title || s.title || 'Session';
      const titleNode = document.getElementById('page-title');
      if (titleNode) titleNode.textContent = shown;
      openTab({ id: s.id, key: s.id, kind: 'session', href: `#/sessions/${s.id}`, title: shown, agent: s.agent });
      toast(t('Local title saved'));
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('edit-local-title').addEventListener('click', () => document.getElementById('local-title-input').focus());
  document.getElementById('add-tag').addEventListener('click', async () => {
    const input = document.getElementById('tag-input');
    try {
      const result = await api(`/sessions/${encodeURIComponent(s.id)}/tags`, { method: 'POST', body: { tag: input.value } });
      tags = result.tags || [];
      input.value = '';
      renderTagList();
      toast(t('Tag added'));
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('tag-input').addEventListener('keydown', (event) => {
    if (event.key === 'Enter') { event.preventDefault(); document.getElementById('add-tag').click(); }
  });
  let offset = 0;
  const append = async () => {
    const box = document.getElementById('transcript');
    const tr = await api(`/sessions/${encodeURIComponent(id)}/transcript?offset=${offset}&limit=100`);
    if (offset === 0) box.innerHTML = '';
    box.insertAdjacentHTML('beforeend', tr.messages.length ? renderTranscriptMessages(tr.messages, s.agent) : (!offset ? `<div class="empty"><strong>${t('No readable messages')}</strong>${t('The host could not parse this transcript.')}</div>` : ''));
    offset = tr.next || 0;
    document.getElementById('load-more-wrap').innerHTML = tr.next ? `<button class="btn" id="load-more">${t('Load earlier messages')}</button>` : '';
    document.getElementById('load-more')?.addEventListener('click', () => append().catch((e) => toast(e.message, true)));
  };
  await append();
}

export async function pageSessionChanges(id) {
  renderShell('sessions');
  const s = await api(`/sessions/${id}`);
  const agent = agentMeta(s.agent);
  const title = s.title || 'Session';
  openTab({ id: `${s.id}:changes`, key: `${s.id}:changes`, kind: 'session', href: `#/sessions/${s.id}/changes`, title: `${title} / Changes`, agent: s.agent });
  const view = setPage(t('Changes'), `${title} · ${projectName(s.cwd)}`, `
    <a class="btn small" href="#/sessions/${s.id}">${svgIcon('sessions')} ${t('Back to session')}</a>
  `);
  let git;
  try {
    git = await api(`/sessions/${s.id}/git`);
  } catch (err) {
    view.innerHTML = `<div class="notice error">${esc(err.message)}</div>`;
    return;
  }
  view.innerHTML = `
    <div class="changes-page changes-main">
      <div class="session-banner">
        ${agentGlyph(s.agent)}
        <div class="session-banner-main">
          <strong>${esc(title)}</strong>
          <span class="session-banner-path">${esc(s.cwd || 'unknown directory')} · ${esc(agent.label)} · ${esc(s.host_name)}</span>
        </div>
        ${stateBadge(s.state)}
        ${git.branch ? `<span class="branch-chip">${svgIcon('branch', 13)}${esc(git.branch)}</span>` : ''}
      </div>
      <div class="changes-head">
        <span class="aside-label">${t('Git changes')}</span>
        ${git.root ? `<code class="mono-small">${esc(git.root)}</code>` : ''}
      </div>
      <p class="hint">${t('Read-only status and diff from')} <code>${esc(s.cwd)}</code>. ${t('No files are modified.')}</p>
      <pre class="changes-status">${esc(git.status?.trim() || t('(clean working tree)'))}${git.stat ? `\n\n${esc(git.stat)}` : ''}</pre>
      ${git.diff ? renderDiffBlock(git.diff) : `<div class="empty compact">${t('No uncommitted changes.')}</div>`}
    </div>`;
}

export async function startRun(session, fork, mode = 'terminal', extra = {}) {
  try {
    const run = await api(`/sessions/${session.id}/runs`, { method: 'POST', body: {
      mode, fork: Boolean(fork), cols: 120, rows: 32, ...extra,
    } });
    location.hash = `#/runs/${run.id}`;
    return run;
  } catch (err) {
    if (err.status === 409 && (err.reason === 'active_run' || err.reason === 'active_elsewhere')) {
      conflictDialog(session, { fork, mode, err });
      return null;
    }
    toast(err.message, true);
    return null;
  }
}

function conflictDialog(session, { fork, mode, err }) {
  const canFork = session.agent === 'codex' || session.agent === 'claude';
  const activeRun = err.reason === 'active_run';
  const description = activeRun
    ? t('Another AgentNexus run already controls this native session. Attach to it instead of opening a second writer.')
    : t('A native terminal appears to have this session open. Starting here could make two writers corrupt the same conversation.');
  modal(`
    <h2>${activeRun ? t('Session is already active') : t('Open in native terminal')}</h2>
    <p class="hint">${esc(description)}</p>
    <div class="notice info small">${t('Native detection is best effort. The original CLI remains the source of truth.')}</div>
    <div class="modal-actions stack">
      ${activeRun && err.runId ? `<button class="btn primary" type="button" id="conflict-attach">${t('Attach to the existing run')}</button>` : ''}
      <a class="btn" href="#/sessions/${esc(session.id)}" id="conflict-readonly">${t('Open read-only transcript')}</a>
      ${!activeRun && !fork && canFork ? `<button class="btn" type="button" id="conflict-fork">${t('Fork the session instead')}</button>` : ''}
      ${!activeRun ? `<button class="btn danger" type="button" id="conflict-force">${t('Resume anyway (step-up required)')}</button>` : ''}
      <button class="btn ghost" type="button" data-close>${t('Cancel')}</button>
    </div>`, () => {});
  document.getElementById('conflict-attach')?.addEventListener('click', () => {
    closeModal();
    location.hash = `#/runs/${err.runId}`;
  });
  document.getElementById('conflict-readonly')?.addEventListener('click', () => closeModal());
  document.getElementById('conflict-fork')?.addEventListener('click', () => {
    closeModal();
    startRun(session, true, mode);
  });
  document.getElementById('conflict-force')?.addEventListener('click', async () => {
    try {
      await ensureStepUp();
      closeModal();
      await startRun(session, false, mode, { force: true });
    } catch (e) {
      toast(e.message, true);
    }
  });
}
