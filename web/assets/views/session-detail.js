import { api } from '../api.js';
import { rememberRecent } from '../palette.js';
import { loadSidebar, renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { openTab } from '../tabs.js';
import { agentGlyph, stateBadge, toast } from '../ui.js';
import { renderDiffBlock, renderTranscriptMessages } from '../components/transcript.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { agentMeta, cwdKey, esc, fmtRelative, fmtTime, projectName, repoLink } from '../ui-format.js';

export async function pageSession(id) {
  renderShell('sessions');
  const view = setPage(t('Session'), t('Loading transcript…'), `<a class="btn small" href="#/sessions">${t('Back')}</a>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const s = await api(`/sessions/${id}`);
  const agent = agentMeta(s.agent);
  let versionWarning = '';
  let structuredAllowed = s.agent === 'opencode' || s.agent === 'codex' || s.agent === 'claude';
  try {
    const { hosts } = await api('/hosts');
    const host = (hosts || []).find((h) => h.id === s.host_id);
    const info = (host?.agents || []).find((a) => a.name === s.agent);
    versionWarning = info?.version_warning || '';
    structuredAllowed = Boolean(info?.capabilities?.includes('structured'));
  } catch { /* keep the previous capability assumption */ }
  openTab({ id: s.id, key: s.id, kind: 'session', href: `#/sessions/${s.id}`, title: s.title || 'Untitled session', agent: s.agent });
  rememberRecent({ id: s.id, title: s.title || 'Untitled session', agent: s.agent, hint: `${agent.label} · ${s.host_name} · ${projectName(s.cwd)}`, href: `#/sessions/${s.id}` });
  const title = s.title || 'Session';
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
            <strong>${esc(title)}</strong>
            <span class="session-banner-path">${esc(s.cwd || 'unknown directory')}</span>
          </div>
          ${stateBadge(s.state)}
          <span class="session-banner-meta">${s.message_count || 0} ${t((s.message_count || 0) === 1 ? 'message' : 'messages')} · ${t('updated')} ${esc(fmtRelative(s.updated_at))}</span>
        </div>
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

export async function startRun(session, fork, mode = 'terminal') {
  try {
    const run = await api(`/sessions/${session.id}/runs`, { method: 'POST', body: {
      mode, fork: Boolean(fork), cols: 120, rows: 32,
    } });
    location.hash = `#/runs/${run.id}`;
  } catch (err) {
    toast(err.message, true);
  }
}
