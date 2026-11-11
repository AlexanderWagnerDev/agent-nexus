import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { agentGlyph, badge, stateBadge } from '../ui.js';
import { newSessionPicker } from '../components/new-session.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { countLabel, cwdKey, esc, fmtDuration, fmtRelative, projectName } from '../ui-format.js';

export async function pageOverview() {
  renderShell('overview');
  const view = setPage(t('Home'), t('Your coding agents across every paired machine.'), `<button class="btn primary small" id="overview-new">${t('New session')}</button><a class="btn small" href="#/runs">${t('Control room')}</a>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const [{ hosts }, { sessions }, { projects }, { runs }] = await Promise.all([
    api('/hosts'),
    api('/sessions?limit=80'),
    api('/projects?limit=200'),
    api('/runs'),
  ]);
  document.getElementById('overview-new').addEventListener('click', newSessionPicker);
  const online = hosts.filter((h) => h.status === 'online');
  const totalSessions = projects.reduce((sum, project) => sum + project.sessions, 0);
  const agents = new Set();
  hosts.forEach((h) => (h.agents || []).forEach((a) => agents.add(a.name)));
  const activeRuns = runs.slice(0, 6);
  const liveRuns = runs.filter((r) => r.state === 'running' || r.state === 'starting' || r.state === 'waiting_for_input');
  const openWorktrees = new Set(runs.map((r) => cwdKey(r.cwd)).filter(Boolean)).size;
  view.innerHTML = `
    <div class="home-head">
      <div>
        <h2 class="home-title">${t('Your workspace')}</h2>
        <p class="home-sub">${online.length ? `${countLabel(online.length, 'machine', 'machines')} ${t('online')}` : t('No machine online')} · ${countLabel(openWorktrees, 'worktree', 'worktrees')} ${t('with open runs')} · ${countLabel(agents.size, 'agent type', 'agent types')}</p>
      </div>
      <div class="home-stats">
        <span><strong>${totalSessions}</strong> ${t(totalSessions === 1 ? 'session' : 'sessions')}</span>
        <span><strong>${projects.length}</strong> ${t(projects.length === 1 ? 'project' : 'projects')}</span>
        <span><strong>${liveRuns.length}</strong> ${t('working')}</span>
      </div>
    </div>

    <section class="home-section">
      <div class="section-head"><h2>${t('Agent activity')}</h2><a class="small" href="#/runs">${t('Open control room')}</a></div>
      ${activeRuns.length ? `<div class="run-board">${activeRuns.map(runCard).join('')}</div>` : `<div class="home-empty"><span>${svgIcon('bot', 22)}</span><div><strong>${t('No open agent runs')}</strong><p>${t('Resume a native session or start a new agent on an online host.')}</p></div><button class="btn primary small" id="home-new">${t('New session')}</button></div>`}
    </section>

    <div class="home-columns">
      <section>
        <div class="section-head"><h2>${t('Projects')}</h2><a class="small" href="#/projects">${t('All projects')}</a></div>
        ${projects.length ? `<div class="project-list">${projects.slice(0, 7).map(projectRow).join('')}</div>` : `<div class="card empty"><strong>${t('No projects yet')}</strong>${t('Pair a host and native sessions will be discovered automatically.')}</div>`}
      </section>
      <section>
        <div class="section-head"><h2>${t('Recent sessions')}</h2><a class="small" href="#/sessions">${t('All sessions')}</a></div>
        ${sessions.length ? `<div class="session-list">${sessions.slice(0, 7).map(sessionRow).join('')}</div>` : `<div class="card empty"><strong>${t('No sessions')}</strong>${t('Pair a host that already has coding-agent history.')}</div>`}
      </section>
    </div>

    <section class="home-section">
      <div class="section-head"><h2>${t('Hosts')}</h2><a class="small" href="#/hosts">${t('Manage hosts')}</a></div>
      ${hosts.length ? `<div class="host-strip">${hosts.map((h) => `<a class="host-pill" href="#/hosts"><span class="side-state ${h.status === 'online' ? 'online' : ''}"></span><span>${esc(h.name)}</span><small>${esc(h.os)}/${esc(h.arch)} · ${(h.agents || []).length} ${t((h.agents || []).length === 1 ? 'agent' : 'agents')}</small></a>`).join('')}</div>` : `<div class="card empty"><strong>${t('No hosts')}</strong>${t('Pair your first machine to start discovering sessions.')}</div>`}
    </section>`;
  document.getElementById('home-new')?.addEventListener('click', newSessionPicker);
}

export function runCard(run) {
  return `<a class="run-card" href="#/runs/${esc(run.id)}">
    <div class="run-card-head">${agentGlyph(run.agent)}${stateBadge(run.state)}<span class="run-elapsed">${fmtDuration(run.started_at)}</span></div>
    <div class="run-card-title">${esc(run.session_title || t('Untitled session'))}</div>
    <div class="run-card-meta">${esc(projectName(run.cwd))} <span>·</span> ${esc(run.host_name || run.host_id)} <span>·</span> ${esc(run.mode)}</div>
  </a>`;
}

export function projectRow(p) {
  return `<a class="project-row" href="#/sessions?project=${encodeURIComponent(p.cwd)}&host=${encodeURIComponent(p.host_id)}">
    <span class="project-row-icon">${svgIcon('folder', 17)}</span>
    <span class="project-row-main"><strong>${esc(p.name)}</strong><small>${esc(p.host_name)}${p.branch ? ` · ${esc(p.branch)}` : ''} · ${countLabel(p.sessions, 'session', 'sessions')}</small></span>
    <span class="project-row-agents">${(p.agents || []).slice(0, 3).map((agent) => agentGlyph(agent, 'tiny')).join('')}${p.active ? `<span class="active-pill">${t('{n} active', { n: p.active })}</span>` : ''}</span>
    <span class="project-row-time">${esc(fmtRelative(p.last_at))}</span>
  </a>`;
}

export function hostRowCompact(h) {
  return `<a class="list-row" href="#/hosts">
    <div><div class="title">${esc(h.name)}</div><div class="sub"><span>${esc(h.os)}/${esc(h.arch)}</span><span>${countLabel((h.agents || []).length, 'agent', 'agents')}</span></div></div>
    <div class="right">${badge(h.status === 'online' ? 'online' : 'offline', h.status)}</div>
  </a>`;
}

export function sessionRow(s) {
  return `<a class="session-row" href="#/sessions/${esc(s.id)}">
    ${agentGlyph(s.agent)}
    <span class="session-row-main">
      <strong>${esc(s.title || t('Untitled session'))}</strong>
      <small>${esc(projectName(s.cwd))} <span>·</span> ${esc(s.host_name)} <span>·</span> ${esc(fmtRelative(s.updated_at))}</small>
    </span>
    ${s.git_branch ? `<span class="branch-chip">${svgIcon('branch', 12)}${esc(s.git_branch)}</span>` : ''}
    ${stateBadge(s.state)}
  </a>`;
}
