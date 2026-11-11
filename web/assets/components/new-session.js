import { api } from '../api.js';
import { closeModal, modal, toast } from '../ui.js';
import { t } from '../i18n.js';
import { agentMeta, agentVersion, esc } from '../ui-format.js';

export async function newSessionPicker() {
  try {
    const { hosts } = await api('/hosts');
    const online = hosts.filter((h) => h.status === 'online');
    if (!online.length) {
      toast(t('No online host. Pair a host first.'), true);
      return;
    }
    if (online.length === 1) {
      await newSessionDialog(online[0]);
      return;
    }
    modal(`
      <h2>${t('New session')}</h2>
      <p class="hint">${t('Choose the machine that should run the agent.')}</p>
      <form class="form" id="host-picker-form">
        <label class="label">${t('Host')}<select class="select" name="host">${online.map((h) => `<option value="${esc(h.id)}">${esc(h.name)} · ${esc(h.os)}/${esc(h.arch)}</option>`).join('')}</select></label>
        <div class="modal-actions"><button class="btn ghost" type="button" data-close>${t('Cancel')}</button><button class="btn primary" type="submit">${t('Continue')}</button></div>
      </form>`);
    document.getElementById('host-picker-form').addEventListener('submit', (event) => {
      event.preventDefault();
      const id = new FormData(event.target).get('host');
      const host = online.find((h) => h.id === id);
      closeModal();
      if (host) newSessionDialog(host);
    });
  } catch (err) { toast(err.message, true); }
}

export async function newSessionDialog(host) {
  const agents = (host?.agents || []).filter((a) => a.supported);
  let projects = [];
  try {
    const { sessions } = await api(`/sessions?host=${encodeURIComponent(host.id)}&limit=200`);
    projects = [...new Set(sessions.map((s) => s.cwd).filter(Boolean))].slice(0, 40);
  } catch { /* the free-text field still works */ }
  modal(`
    <h2>${t('New session on')} ${esc(host?.name || 'host')}</h2>
    <p class="hint">${t('The agent starts in a directory permitted by the host policy. Permission-bypass flags are rejected on the host.')}</p>
    <form class="form" id="new-session-form">
      <label class="label">${t('Agent')}<select class="select" name="agent" id="new-agent">${agents.map((a) => `<option value="${esc(a.name)}" data-structured="${(a.capabilities || []).includes('structured') ? '1' : '0'}">${esc(agentMeta(a.name).label)} — ${esc(agentVersion(a.name, a.version))}</option>`).join('')}</select></label>
      <label class="label">${t('Mode')}<select class="select" name="mode" id="new-mode"><option value="structured">${t('Structured chat')}</option><option value="terminal">${t('Terminal')}</option></select></label>
      <label class="label">${t('Working directory')}<input class="input" name="cwd" list="project-options" placeholder="${t('C:\\Users\\me\\project or /home/me/project')}" required><datalist id="project-options">${projects.map((p) => `<option value="${esc(p)}"></option>`).join('')}</datalist></label>
      <div class="notice info">${t('The host enforces its own')} <code>allowed_roots</code>; ${t('a path outside them is rejected even if this form accepts it.')}</div>
      <div class="modal-actions">
        <button class="btn ghost" type="button" data-close>${t('Cancel')}</button>
        <button class="btn primary" type="submit">${t('Start session')}</button>
      </div>
    </form>`);
  const agentSelect = document.getElementById('new-agent');
  const modeSelect = document.getElementById('new-mode');
  const syncMode = () => {
    const option = agentSelect.selectedOptions[0];
    const structured = option?.dataset.structured === '1';
    modeSelect.querySelector('option[value=structured]').disabled = !structured;
    if (!structured) modeSelect.value = 'terminal';
  };
  agentSelect.addEventListener('change', syncMode);
  modeSelect.value = agentSelect.selectedOptions[0]?.dataset.structured === '1' ? 'structured' : 'terminal';
  syncMode();
  document.getElementById('new-session-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = new FormData(event.target);
    try {
      const run = await api(`/hosts/${host.id}/runs`, { method: 'POST', body: {
        agent: form.get('agent'), cwd: form.get('cwd'), mode: form.get('mode'), cols: 120, rows: 32,
      } });
      closeModal();
      location.hash = `#/runs/${run.id}`;
    } catch (err) {
      toast(err.message, true);
    }
  });
}
