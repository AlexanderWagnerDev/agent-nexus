import { state } from '../state.js';
import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { agentGlyph, badge } from '../ui.js';
import { newSessionPicker } from '../components/new-session.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { countLabel, esc, fmtRelative, groupProjects, repoLink } from '../ui-format.js';

function repoGroups(projects) {
  return groupProjects(projects);
}

export async function pageProjects() {
  const pageGen = state.pageGen;
  renderShell('projects');
  const view = setPage(t('Projects'), t('Working directories discovered from native sessions, grouped by repository across hosts.'), `<button class="btn primary small" id="projects-new">${t('New session')}</button>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const { projects } = await api('/projects?limit=500');
  if (pageGen !== state.pageGen) return;
  document.getElementById('projects-new').addEventListener('click', newSessionPicker);
  let filter = { query: '', mode: 'repos' };

  const render = () => {
    const list = document.getElementById('project-list');
    if (!list) return;
    const query = filter.query.trim().toLowerCase();
    const matches = (value) => !query || String(value).toLowerCase().includes(query);
    if (filter.mode === 'repos') {
      const repos = repoGroups(projects).filter((repo) => matches(`${repo.name} ${repo.url} ${repo.hosts.map((h) => h.name).join(' ')} ${repo.projects.map((p) => p.cwd).join(' ')}`));
      list.innerHTML = repos.length ? `<div class="repo-grid">${repos.map((repo) => `
        <article class="repo-card">
          <div class="repo-head">
            <div class="project-icon">${svgIcon('branch', 20)}</div>
            <div class="project-main">
              <div class="project-title">${esc(repo.name)}${repo.active ? `<span class="active-pill">${t('{n} active', { n: repo.active })}</span>` : ''}${repo.hosts.length > 1 ? `<span class="chip">${countLabel(repo.hosts.length, 'host', 'hosts')}</span>` : ''}</div>
              <div class="project-path">${repo.url ? repoLink(repo.url) : esc(repo.projects[0].cwd)}</div>
              <div class="project-meta">${repo.hosts.map((host) => esc(host.name)).join(', ')} · ${countLabel(repo.projects.length, 'worktree', 'worktrees')} · ${countLabel(repo.sessions, 'session', 'sessions')} · ${esc(fmtRelative(repo.lastAt))}</div>
            </div>
          </div>
          <div class="repo-worktrees">${repo.projects.map((project) => `
            <a class="repo-worktree" href="#/sessions?project=${encodeURIComponent(project.cwd)}&host=${encodeURIComponent(project.host_id)}">
              <span class="mono-small">${esc(project.host_name)} · ${esc(project.cwd)}</span>
              <span class="split">${project.branch ? `<span class="branch-chip">${svgIcon('branch', 11)}${esc(project.branch)}</span>` : ''}${project.active ? badge(t('{n} active', { n: project.active }), 'running') : ''}</span>
            </a>`).join('')}</div>
        </article>`).join('')}</div>` : `<div class="card empty"><strong>${t('No repositories match')}</strong>${t('Try a different filter or wait for a host to reconnect.')}</div>`;
      return;
    }
    const filtered = projects.filter((p) => matches(`${p.name} ${p.cwd} ${p.host_name} ${p.branch}`));
    list.innerHTML = filtered.length ? `<div class="project-grid">${filtered.map((p) => `
      <a class="project-card" href="#/sessions?project=${encodeURIComponent(p.cwd)}&host=${encodeURIComponent(p.host_id)}">
        <div class="project-icon">${svgIcon('folder', 20)}</div>
        <div class="project-main">
          <div class="project-title">${esc(p.name)}${p.active ? `<span class="active-pill">${t('{n} active', { n: p.active })}</span>` : ''}${p.active > 1 ? `<span class="warn-pill" title="${t('More than one agent is active in this working tree')}">${t('shared worktree')}</span>` : ''}</div>
          <div class="project-path">${esc(p.cwd)}</div>
          <div class="project-meta">${esc(p.host_name)}${p.branch ? ` · ${svgIcon('branch', 12)} ${esc(p.branch)}` : ''} · ${countLabel(p.sessions, 'session', 'sessions')} · ${esc(fmtRelative(p.last_at))}</div>
        </div>
        <div class="project-agents">${(p.agents || []).slice(0, 4).map((agent) => agentGlyph(agent, 'small')).join('')}</div>
        <span class="project-arrow">${svgIcon('chevronRight')}</span>
      </a>`).join('')}</div>` : `<div class="card empty"><strong>${t('No projects match')}</strong>${t('Try a different filter or wait for a host to reconnect.')}</div>`;
  };
  view.innerHTML = `
    <div class="toolbar">
      <input class="input grow" id="projects-filter" placeholder="${t('Filter projects, paths, hosts or branches…')}">
      <select class="select" id="projects-mode" aria-label="${t('Group projects by')}"><option value="worktrees">${t('Worktrees')}</option><option value="repos" selected>${t('Repositories')}</option></select>
      <span class="toolbar-count" id="projects-count">${countLabel(repoGroups(projects).length, 'repository', 'repositories')}</span>
    </div>
    <div id="project-list"></div>`;
  document.getElementById('projects-filter').addEventListener('input', (event) => { filter.query = event.target.value; render(); });
  document.getElementById('projects-mode').addEventListener('change', (event) => {
    filter.mode = event.target.value;
    document.getElementById('projects-count').textContent = filter.mode === 'repos'
      ? countLabel(repoGroups(projects).length, 'repository', 'repositories')
      : countLabel(projects.length, 'worktree', 'worktrees');
    render();
  });
  render();
}
