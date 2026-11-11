import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { agentGlyph } from '../ui.js';
import { newSessionPicker } from '../components/new-session.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { esc, fmtRelative } from '../ui-format.js';

export async function pageProjects() {
  renderShell('projects');
  const view = setPage(t('Projects'), t('Working directories discovered from native sessions, grouped by host and worktree.'), `<button class="btn primary small" id="projects-new">${t('New session')}</button>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  const { projects } = await api('/projects?limit=500');
  document.getElementById('projects-new').addEventListener('click', newSessionPicker);
  const render = (filter = '') => {
    const list = document.getElementById('project-list');
    if (!list) return;
    const query = filter.trim().toLowerCase();
    const filtered = projects.filter((p) => !query || `${p.name} ${p.cwd} ${p.host_name} ${p.branch}`.toLowerCase().includes(query));
    list.innerHTML = filtered.length ? `<div class="project-grid">${filtered.map((p) => `
      <a class="project-card" href="#/sessions?project=${encodeURIComponent(p.cwd)}&host=${encodeURIComponent(p.host_id)}">
        <div class="project-icon">${svgIcon('folder', 20)}</div>
        <div class="project-main">
          <div class="project-title">${esc(p.name)}${p.active ? `<span class="active-pill">${t('{n} active', { n: p.active })}</span>` : ''}${p.active > 1 ? `<span class="warn-pill" title="${t('More than one agent is active in this working tree')}">${t('shared worktree')}</span>` : ''}</div>
          <div class="project-path">${esc(p.cwd)}</div>
          <div class="project-meta">${esc(p.host_name)}${p.branch ? ` · ${svgIcon('branch', 12)} ${esc(p.branch)}` : ''} · ${t('{n} sessions', { n: p.sessions })} · ${esc(fmtRelative(p.last_at))}</div>
        </div>
        <div class="project-agents">${(p.agents || []).slice(0, 4).map((agent) => agentGlyph(agent, 'small')).join('')}</div>
        <span class="project-arrow">${svgIcon('chevronRight')}</span>
      </a>`).join('')}</div>` : `<div class="card empty"><strong>${t('No projects match')}</strong>${t('Try a different filter or wait for a host to reconnect.')}</div>`;
  };
  view.innerHTML = `
    <div class="toolbar">
      <input class="input grow" id="projects-filter" placeholder="Filter projects, paths, hosts or branches…">
      <span class="toolbar-count">${t('{n} worktrees', { n: projects.length })}</span>
    </div>
    <div id="project-list"></div>`;
  document.getElementById('projects-filter').addEventListener('input', (event) => render(event.target.value));
  render();
}
