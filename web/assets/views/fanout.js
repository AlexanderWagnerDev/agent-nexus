import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { agentMeta, countLabel, esc, fmtStamp, projectName } from '../ui-format.js';
import { badge, toast } from '../ui.js';
import { t } from '../i18n.js';
import { svgIcon } from '../icons.js';

const AGENTS = ['codex', 'opencode', 'claude', 'gemini'];

function targetRow(target, hosts) {
  return `<div class="fanout-target" data-target>
    <select class="select" data-target-host aria-label="${t('Host')}">${hosts.map((host) => `<option value="${esc(host.id)}" ${target.host_id === host.id ? 'selected' : ''}>${esc(host.name)}</option>`).join('')}</select>
    <select class="select" data-target-agent aria-label="${t('Agent')}">${AGENTS.map((agent) => `<option value="${agent}" ${target.agent === agent ? 'selected' : ''}>${agentMeta(agent).label}</option>`).join('')}</select>
    <input class="input" data-target-cwd placeholder="${t('Absolute working directory on that host')}" value="${esc(target.cwd || '')}">
    <button class="icon-btn" type="button" data-remove-target aria-label="${t('Remove target')}">${svgIcon('x', 13)}</button>
  </div>`;
}

function runCard(run, git, overlap) {
  const changed = git?.file_count || 0;
  return `<article class="card fanout-run">
    <div class="split wrap"><strong>${esc(run.host_name || run.host_id)} · ${esc(agentMeta(run.agent).label)}</strong>${badge(t(run.state || 'unknown'), run.state || 'unknown')}</div>
    <div class="muted small">${esc(run.cwd || '')}</div>
    <div class="muted small">${esc(run.title || t('Waiting for native session'))}</div>
    ${git ? `<div class="muted small">${t('{n} changed files', { n: changed })}${git.branch ? ` · ${esc(git.branch)}` : ''}${git.status_truncated ? ' · truncated' : ''}</div>` : ''}
    ${overlap.length ? `<div class="notice error small">${t('Files also changed by another run')}: ${overlap.map(esc).join(', ')}</div>` : ''}
    ${run.error ? `<div class="notice error small">${esc(run.error)}</div>` : ''}
    <div class="split wrap mt-2">
      <a class="btn small" href="#/runs/${esc(run.run_id)}">${t('Open chat')}</a>
      ${run.session_id ? `<a class="btn small" href="#/sessions/${esc(run.session_id)}/changes">${t('Review changes')}</a>` : ''}
    </div>
  </article>`;
}

function statusFiles(status) {
  return String(status || '').split('\n').map((line) => line.trim()).filter(Boolean).map((line) => {
    let path = line.slice(3).trim();
    if (path.includes(' -> ')) path = path.split(' -> ').pop();
    return path.replace(/^"|"$/g, '');
  }).filter(Boolean);
}

export async function pageFanout() {
  const pageGen = state.pageGen;
  renderShell('fanout');
  const [{ hosts }, { projects }, { fanouts }] = await Promise.all([
    api('/hosts').catch(() => ({ hosts: [] })),
    api('/projects?limit=200').catch(() => ({ projects: [] })),
    api('/fanouts').catch(() => ({ fanouts: [] })),
  ]);
  if (pageGen !== state.pageGen) return;
  const view = setPage(t('Fan-out'), t('Send the same prompt to independent native sessions on several hosts and compare the results.'));
  const hostList = hosts || [];
  let targets = hostList.length ? [{ host_id: hostList[0].id, agent: 'codex', cwd: '' }] : [];
  const projectRows = (projects || []).filter((project) => project.host_id);
  view.innerHTML = `
    <section class="card">
      <label class="label">${t('Prompt')}<textarea class="input" id="fanout-prompt" rows="4" placeholder="${t('What should every agent do?')}"></textarea></label>
      <div class="split wrap mt-2">
        <label class="label-inline">${t('Add a project target')}<select class="select" id="fanout-project"><option value="">${t('Choose a project')}</option>${projectRows.map((project, index) => `<option value="${index}">${esc(projectName(project.cwd))} · ${esc(project.host_name)} · ${esc(project.cwd)}</option>`).join('')}</select></label>
        <button class="btn small" type="button" id="fanout-add">${svgIcon('plus', 13)} ${t('Add target')}</button>
      </div>
      <div class="fanout-targets" id="fanout-targets">${targets.map((target) => targetRow(target, hostList)).join('')}</div>
      <p class="hint">${t('Each target starts a new native structured session. Two targets on the same host and working directory are rejected to avoid competing writes.')}</p>
      <button class="btn primary" type="button" id="fanout-start" ${hostList.length ? '' : 'disabled'}>${t('Start fan-out')}</button>
      <div id="fanout-errors" class="mt-2"></div>
    </section>
    <section class="card" id="fanout-results" hidden>
      <div class="split"><h2 class="mt-0">${t('Fan-out result')}</h2><span class="muted small" id="fanout-created"></span></div>
      <div class="fanout-grid" id="fanout-runs"></div>
      <div id="fanout-conflicts" class="mt-2"></div>
    </section>
    <section class="card">
      <h2 class="mt-0">${t('Recent fan-outs')}</h2>
      ${(fanouts || []).length ? `<div class="fanout-history">${fanouts.map((item) => `<button class="fanout-history-row" type="button" data-fanout="${esc(item.id)}"><span>${esc(item.prompt)}</span><small>${esc(fmtStamp(item.created_at))} · ${countLabel(item.runs.length, 'run', 'runs')}</small></button>`).join('')}</div>` : `<p class="muted">${t('No fan-outs yet.')}</p>`}
    </section>`;

  const targetsEl = document.getElementById('fanout-targets');
  const renderTargets = () => {
    targetsEl.innerHTML = targets.map((target) => targetRow(target, hostList)).join('');
  };
  targetsEl.addEventListener('input', (event) => {
    const row = event.target.closest('[data-target]');
    if (!row) return;
    const index = [...targetsEl.querySelectorAll('[data-target]')].indexOf(row);
    if (index < 0) return;
    if (event.target.matches('[data-target-host]')) targets[index].host_id = event.target.value;
    if (event.target.matches('[data-target-agent]')) targets[index].agent = event.target.value;
    if (event.target.matches('[data-target-cwd]')) targets[index].cwd = event.target.value;
  });
  targetsEl.addEventListener('click', (event) => {
    const button = event.target.closest('[data-remove-target]');
    if (!button) return;
    const row = button.closest('[data-target]');
    const index = [...targetsEl.querySelectorAll('[data-target]')].indexOf(row);
    if (index >= 0) {
      targets.splice(index, 1);
      renderTargets();
    }
  });
  document.getElementById('fanout-add').addEventListener('click', () => {
    targets.push({ host_id: hostList[0]?.id || '', agent: 'codex', cwd: '' });
    renderTargets();
  });
  document.getElementById('fanout-project').addEventListener('change', (event) => {
    if (event.target.value === '') return;
    const project = projectRows[Number(event.target.value)];
    if (!project) return;
    targets.push({ host_id: project.host_id, agent: (project.agents || [])[0] || 'codex', cwd: project.cwd });
    renderTargets();
    event.target.value = '';
  });

  let poll = null;
  const gitCache = new Map();
  const showFanout = async (id) => {
    const data = await api(`/fanouts/${encodeURIComponent(id)}`);
    if (pageGen !== state.pageGen) return;
    await Promise.all(data.runs.map(async (run) => {
      if (!run.session_id || gitCache.has(run.session_id)) return;
      try {
        gitCache.set(run.session_id, await api(`/sessions/${encodeURIComponent(run.session_id)}/git?stat=1`));
      } catch {
        gitCache.set(run.session_id, null);
      }
    }));
    const byFile = new Map();
    for (const run of data.runs) {
      const git = gitCache.get(run.session_id);
      for (const file of statusFiles(git?.status)) {
        if (!byFile.has(file)) byFile.set(file, []);
        byFile.get(file).push(run.host_name || run.host_id || 'run');
      }
    }
    const shared = new Set([...byFile.entries()].filter(([, hosts]) => hosts.length > 1).map(([file]) => file));
    document.getElementById('fanout-results').hidden = false;
    document.getElementById('fanout-created').textContent = fmtStamp(data.created_at);
    document.getElementById('fanout-runs').innerHTML = data.runs.map((run) => {
      const overlap = [...shared].filter((file) => statusFiles(gitCache.get(run.session_id)?.status).includes(file));
      return runCard(run, gitCache.get(run.session_id), overlap);
    }).join('');
    const conflicts = document.getElementById('fanout-conflicts');
    if (conflicts) {
      conflicts.innerHTML = shared.size ? `<div class="notice error small"><strong>${t('Overlapping files')}</strong><div class="mono-small">${[...shared].map(esc).join('<br>')}</div></div>` : '';
    }
    const active = data.runs.some((run) => !['exited', 'completed', 'failed'].includes(run.state));
    clearInterval(poll);
    if (active) poll = setInterval(() => showFanout(id).catch(() => {}), 3000);
  };
  document.getElementById('fanout-start').addEventListener('click', async () => {
    const prompt = document.getElementById('fanout-prompt').value.trim();
    const clean = targets.filter((target) => target.cwd.trim());
    if (!prompt || !clean.length) { toast(t('Add a prompt and at least one target with a working directory'), true); return; }
    const button = document.getElementById('fanout-start');
    button.disabled = true;
    try {
      const result = await api('/fanouts', { method: 'POST', body: { prompt, targets: clean } });
      const errors = result.errors || [];
      document.getElementById('fanout-errors').innerHTML = errors.length ? `<div class="notice error small">${errors.map(esc).join('<br>')}</div>` : '';
      toast(t('Fan-out started'));
      await showFanout(result.fanout.id);
    } catch (err) {
      toast(err.message, true);
    } finally {
      button.disabled = false;
    }
  });
  view.querySelectorAll('[data-fanout]').forEach((button) => button.addEventListener('click', () => {
    showFanout(button.dataset.fanout).catch((err) => toast(err.message, true));
  }));
  state.pageCleanup = () => clearInterval(poll);
}
