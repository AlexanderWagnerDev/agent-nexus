import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { badge, closeModal, confirmDanger, modal, toast } from '../ui.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';
import { esc, fmtRelative } from '../ui-format.js';

const statuses = ['draft', 'ready', 'queued', 'running', 'waiting_for_approval', 'blocked', 'completed', 'failed', 'cancelled'];
const priorities = ['low', 'normal', 'high', 'urgent'];

function statusClass(status) {
  if (status === 'completed') return 'online';
  if (status === 'running') return 'running';
  if (status === 'waiting_for_approval' || status === 'queued') return 'waiting_for_input';
  if (status === 'failed') return 'failed';
  return 'idle';
}

function statusLabel(status) {
  return t(status.replaceAll('_', ' '));
}

function hostOptions(hosts, selected) {
  return `<option value="">${t('No host')}</option>${hosts.map((h) => `<option value="${esc(h.id)}" ${h.id === selected ? 'selected' : ''}>${esc(h.name)}${h.status === 'online' ? '' : ` (${t('offline')})`}</option>`).join('')}`;
}

async function loadTasks() {
  const [{ tasks, total }, { hosts }] = await Promise.all([api('/tasks?limit=500'), api('/hosts')]);
  return { tasks, total, hosts };
}

export async function pageTasks() {
  const pageGen = state.pageGen;
  renderShell('tasks');
  const view = setPage(t('Tasks'), t('Delegated work with dependencies, runs and review state. Tasks never bypass host policies.'), `<button class="btn primary small" id="task-new">${t('New task')}</button>`);
  view.innerHTML = '<div class="empty"><span class="spinner"></span></div>';
  let data;
  try {
    data = await loadTasks();
  } catch (err) {
    if (pageGen !== state.pageGen) return;
    view.innerHTML = `<div class="card empty"><strong>${t('Could not load tasks')}</strong>${esc(err.message)}</div>`;
    return;
  }
  if (pageGen !== state.pageGen) return;
  const { hosts } = data;
  let filter = { status: '', query: '' };

  const render = () => {
    const list = document.getElementById('task-list');
    if (!list) return;
    const query = filter.query.trim().toLowerCase();
    const tasks = data.tasks.filter((task) => {
      if (filter.status && task.status !== filter.status) return false;
      if (!query) return true;
      return `${task.title} ${task.description} ${task.project} ${task.host_name} ${task.agent}`.toLowerCase().includes(query);
    });
    if (!tasks.length) {
      list.innerHTML = `<div class="card empty"><strong>${t('No tasks match')}</strong>${t('Create a task to delegate work to a coding agent.')}</div>`;
      return;
    }
    list.innerHTML = `<div class="table-wrap"><table class="data-table task-table">
      <thead><tr><th>${t('Task')}</th><th>${t('Status')}</th><th>${t('Priority')}</th><th>${t('Project')}</th><th>${t('Host')}</th><th>${t('Agent')}</th><th>${t('Dependencies')}</th><th>${t('Updated')}</th></tr></thead>
      <tbody>${tasks.map((task) => `
        <tr class="task-row" data-task="${esc(task.id)}" tabindex="0">
          <td><span class="task-title">${esc(task.title)}</span>${task.error ? `<small class="task-error">${esc(task.error)}</small>` : ''}</td>
          <td>${badge(statusLabel(task.status), statusClass(task.status))}</td>
          <td><span class="task-priority ${esc(task.priority)}">${esc(t(task.priority))}</span></td>
          <td class="mono-small wrap">${esc(task.project || '—')}</td>
          <td>${esc(task.host_name || '—')}</td>
          <td>${esc(task.agent || '—')}</td>
          <td>${(task.depends_on || []).length}</td>
          <td class="mono-small">${esc(fmtRelative(task.updated_at))}</td>
        </tr>`).join('')}</tbody>
    </table></div>`;
    list.querySelectorAll('[data-task]').forEach((row) => {
      row.addEventListener('click', () => openTask(row.dataset.task, hosts));
      row.addEventListener('keydown', (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          openTask(row.dataset.task, hosts);
        }
      });
    });
  };

  view.innerHTML = `
    <div class="toolbar">
      <input class="input grow" id="task-search" placeholder="${t('Filter tasks, projects, hosts or agents…')}">
      <select class="select" id="task-status"><option value="">${t('All statuses')}</option>${statuses.map((s) => `<option value="${s}">${esc(statusLabel(s))}</option>`).join('')}</select>
      <span class="toolbar-count">${t('{n} tasks', { n: data.total })}</span>
    </div>
    <div id="task-list"></div>`;
  document.getElementById('task-search').addEventListener('input', (event) => { filter.query = event.target.value; render(); });
  document.getElementById('task-status').addEventListener('change', (event) => { filter.status = event.target.value; render(); });
  document.getElementById('task-new').addEventListener('click', () => openNewTask(hosts));
  render();

  state.wsHandlers.add((msg) => {
    if (msg.type === 'run.state' || msg.type === 'run.exit') {
      const row = document.querySelector(`[data-task]`);
      if (row) loadTasks().then((next) => { data = next; render(); }).catch(() => {});
    }
  });
}

function taskFields(hosts, task = {}) {
  const agentOptions = new Set();
  hosts.forEach((h) => (h.agents || []).forEach((a) => agentOptions.add(a.name)));
  return `
    <label class="label">${t('Title')}<input class="input" id="task-title" maxlength="200" value="${esc(task.title || '')}"></label>
    <label class="label">${t('Description / prompt')}<textarea class="input" id="task-description" rows="6">${esc(task.description || '')}</textarea></label>
    <div class="form-grid">
      <label class="label">${t('Project directory')}<input class="input" id="task-project" value="${esc(task.project || '')}" placeholder="/path/to/repository"></label>
      <label class="label">${t('Host')}<select class="select" id="task-host">${hostOptions(hosts, task.host_id)}</select></label>
      <label class="label">${t('Agent')}<input class="input" id="task-agent" list="task-agents" value="${esc(task.agent || '')}"><datalist id="task-agents">${[...agentOptions].map((a) => `<option value="${esc(a)}">`).join('')}</datalist></label>
      <label class="label">${t('Priority')}<select class="select" id="task-priority">${priorities.map((p) => `<option value="${p}" ${task.priority === p ? 'selected' : ''}>${esc(t(p))}</option>`).join('')}</select></label>
      <label class="label">${t('Status')}<select class="select" id="task-status-field">${task.status === 'running' || task.status === 'waiting_for_approval' ? `<option value="${esc(task.status)}" selected disabled>${esc(statusLabel(task.status))}</option>` : ''}${statuses.filter((s) => s !== 'running' && s !== 'waiting_for_approval').map((s) => `<option value="${s}" ${task.status === s ? 'selected' : ''}>${esc(statusLabel(s))}</option>`).join('')}</select></label>
      <label class="label">${t('Tags')}<input class="input" id="task-tags" value="${esc((task.tags || []).join(', '))}" placeholder="review, backend"></label>
    </div>`;
}

function readTaskFields() {
  return {
    title: document.getElementById('task-title').value.trim(),
    description: document.getElementById('task-description').value.trim(),
    project: document.getElementById('task-project').value.trim(),
    host_id: document.getElementById('task-host').value,
    agent: document.getElementById('task-agent').value.trim(),
    priority: document.getElementById('task-priority').value,
    status: document.getElementById('task-status-field').value,
    tags: document.getElementById('task-tags').value.split(',').map((x) => x.trim()).filter(Boolean),
  };
}

function openNewTask(hosts) {
  modal(`
    <h2>${t('New task')}</h2>
    <p class="hint">${t('The task is a planning record. Delegation starts a native session run and sends its description as the prompt.')}</p>
    ${taskFields(hosts)}
    <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Cancel')}</button><button class="btn primary" type="button" id="task-create">${t('Create task')}</button></div>`);
  document.getElementById('task-create').addEventListener('click', async () => {
    const body = readTaskFields();
    if (!body.title) { toast(t('Title is required'), true); return; }
    try {
      const created = await api('/tasks', { method: 'POST', body });
      toast(t('Task created'));
      closeModal();
      if (created.status === 'ready') openTask(created.id, hosts);
      else pageTasks();
    } catch (err) { toast(err.message, true); }
  });
}

async function openTask(id, hosts) {
  let detail;
  try {
    detail = await api(`/tasks/${encodeURIComponent(id)}`);
  } catch (err) {
    toast(err.message, true);
    return;
  }
  const task = detail.task;
  const all = await api('/tasks?limit=500').catch(() => ({ tasks: [] }));
  const others = all.tasks.filter((other) => other.id !== task.id);
  const deps = (detail.dependencies || []).map((dep) => `<div class="dep-row"><a href="#" data-open-task="${esc(dep.id)}">${esc(dep.title)}</a>${badge(statusLabel(dep.status), statusClass(dep.status))}<button class="icon-btn" type="button" title="${t('Remove')}" data-remove-dep="${esc(dep.id)}">${svgIcon('x', 13)}</button></div>`).join('');
  const dependents = (detail.dependents || []).map((dep) => `<div class="dep-row"><a href="#" data-open-task="${esc(dep.id)}">${esc(dep.title)}</a>${badge(statusLabel(dep.status), statusClass(dep.status))}</div>`).join('');
  const isClosed = task.status === 'completed' || task.status === 'cancelled';
  modal(`
    <div class="modal-title-row"><h2>${esc(task.title)}</h2>${badge(statusLabel(task.status), statusClass(task.status))}</div>
    <p class="hint">${t('Created')} ${esc(fmtRelative(task.created_at))}${task.host_name ? ` · ${esc(task.host_name)}` : ''}${task.run_id ? ` · <a href="#/runs/${esc(task.run_id)}">${t('Open run')}</a>` : ''}${task.session_id ? ` · <a href="#/sessions/${esc(task.session_id)}">${t('Open session')}</a>` : ''}</p>
    <div id="task-edit">${taskFields(hosts, task)}</div>
    <div class="split mt-2">
      <button class="btn primary" type="button" id="task-save">${t('Save')}</button>
      <button class="btn" type="button" id="task-delegate" ${isClosed ? 'disabled' : ''}>${t('Delegate to agent')}</button>
      <button class="btn danger" type="button" id="task-delete">${t('Delete')}</button>
    </div>
    <div class="aside-label mt-3">${t('Dependencies')}</div>
    <div class="dep-list">${deps || `<span class="aside-note">${t('No dependencies')}</span>`}</div>
    <div class="split mt-1">
      <select class="select" id="task-add-dep"><option value="">${t('Add dependency…')}</option>${others.map((o) => `<option value="${esc(o.id)}">${esc(o.title)}</option>`).join('')}</select>
      <button class="btn small" type="button" id="task-add-dep-btn">${t('Add')}</button>
    </div>
    ${dependents ? `<div class="aside-label mt-2">${t('Blocked by this task')}</div><div class="dep-list">${dependents}</div>` : ''}
    <div class="aside-label mt-3">${t('Delegation')}</div>
    <div id="task-delegate-box" class="task-delegate-box" hidden>
      <div class="form-grid">
        <label class="label">${t('Mode')}<select class="select" id="task-delegate-mode"><option value="new">${t('New session')}</option><option value="resume">${t('Resume session')}</option><option value="fork">${t('Fork session')}</option></select></label>
        <label class="label">${t('Session ID (resume/fork)')}<input class="input" id="task-delegate-session" value="${esc(task.session_id || '')}"></label>
      </div>
      <label class="label">${t('Prompt')}<textarea class="input" id="task-delegate-prompt" rows="4">${esc(task.description || task.title)}</textarea></label>
      <button class="btn primary" type="button" id="task-delegate-start">${t('Start run')}</button>
    </div>
    <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Close')}</button></div>`);

  document.querySelectorAll('[data-open-task]').forEach((link) => link.addEventListener('click', (event) => {
    event.preventDefault();
    openTask(link.dataset.openTask, hosts);
  }));
  document.getElementById('task-save').addEventListener('click', async () => {
    try {
      const body = readTaskFields();
      if (task.status === 'running' || task.status === 'waiting_for_approval') delete body.status;
      await api(`/tasks/${encodeURIComponent(task.id)}`, { method: 'PATCH', body });
      toast(t('Task saved'));
      closeModal();
      pageTasks();
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('task-delegate').addEventListener('click', () => {
    const box = document.getElementById('task-delegate-box');
    if (box) box.hidden = !box.hidden;
  });
  document.getElementById('task-delegate-start')?.addEventListener('click', async () => {
    const mode = document.getElementById('task-delegate-mode').value;
    const sessionID = document.getElementById('task-delegate-session').value.trim();
    const prompt = document.getElementById('task-delegate-prompt').value.trim();
    try {
      const result = await api(`/tasks/${encodeURIComponent(task.id)}/delegate`, {
        method: 'POST',
        body: { mode, session_id: sessionID, prompt, run_mode: 'structured' },
      });
      toast(t('Task delegated'));
      closeModal();
      if (result.run?.id) location.hash = `#/runs/${result.run.id}`;
      else pageTasks();
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('task-delete').addEventListener('click', () => {
    confirmDanger(t('Delete this task? The native session and its transcript are not deleted.'), async () => {
      try {
        await api(`/tasks/${encodeURIComponent(task.id)}`, { method: 'DELETE' });
        toast(t('Task deleted'));
        pageTasks();
      } catch (err) { toast(err.message, true); }
    });
  });
  document.getElementById('task-add-dep-btn').addEventListener('click', async () => {
    const dependsOn = document.getElementById('task-add-dep').value;
    if (!dependsOn) return;
    try {
      await api(`/tasks/${encodeURIComponent(task.id)}/dependencies`, { method: 'POST', body: { depends_on: dependsOn } });
      toast(t('Dependency added'));
      openTask(task.id, hosts);
    } catch (err) { toast(err.message, true); }
  });
  document.querySelectorAll('[data-remove-dep]').forEach((button) => button.addEventListener('click', async () => {
    try {
      await api(`/tasks/${encodeURIComponent(task.id)}/dependencies/${encodeURIComponent(button.dataset.removeDep)}`, { method: 'DELETE' });
      openTask(task.id, hosts);
    } catch (err) { toast(err.message, true); }
  }));
}
