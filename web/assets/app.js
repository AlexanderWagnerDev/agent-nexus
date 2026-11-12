/* AgentNexus browser client - bootstrap only. */
import { initLang } from './i18n.js';
import { state } from './state.js';
import { app } from './ui.js';
import { esc } from './ui-format.js';
import { api } from './api.js';
import { setUnauthorizedHook, setStepUpHook } from './api.js';
import { parseRoute } from './routes.js';
import { route, setPageHandler } from './router.js';
import { installGlobalUi, loadUserTheme, renderShell } from './shell.js';
import { renderAuth, renderDevicePair, ensureStepUp } from './auth.js';
import { pageOverview } from './views/overview.js';
import { pageHosts } from './views/hosts.js';
import { pageProjects } from './views/projects.js';
import { pageSessions } from './views/sessions.js';
import { pageUsage } from './views/usage.js';
import { pageSession, pageSessionChanges } from './views/session-detail.js';
import { pageSearch } from './views/search.js';
import { pageSettings } from './views/settings.js';
import { pageDevices } from './views/devices.js';
import { pageUsers } from './views/users.js';
import { pageAudit } from './views/audit.js';
import { pageRuns } from './views/runs.js';
import { pageWorkspace } from './views/workspace.js';
import { pageRun } from './views/run.js';

initLang();
setUnauthorizedHook(() => renderAuth('login'));
setStepUpHook(() => ensureStepUp());

const pages = {
  overview: pageOverview,
  hosts: pageHosts,
  projects: pageProjects,
  sessions: pageSessions,
  usage: pageUsage,
  session: pageSession,
  sessionChanges: pageSessionChanges,
  search: pageSearch,
  settings: pageSettings,
  devices: pageDevices,
  users: pageUsers,
  audit: pageAudit,
  runs: pageRuns,
  workspace: pageWorkspace,
  run: pageRun,
};

setPageHandler(async () => {
  const target = parseRoute(location.hash);
  return await pages[target.page](target.id);
});
async function boot() {
  state.embed = new URLSearchParams(location.search).get('embed') === '1';
  if (state.embed) document.documentElement.dataset.embed = '1';
  installGlobalUi();
  if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {});
  const pairToken = new URLSearchParams(location.search).get('pair');
  if (pairToken) return renderDevicePair(pairToken);
  try {
    const status = await api('/setup/status', { allow401: true });
    state.setupRequired = status.setup_required;
    state.hubName = status.hub_name || state.hubName;
    state.version = status.version || state.version;
    if (status.setup_required) return renderAuth('setup');
    try {
      state.me = await api('/me', { allow401: true });
      await loadUserTheme();
      renderShell('overview');
      await route();
    } catch {
      renderAuth('login');
    }
  } catch (err) {
    app.innerHTML = `<div class="auth-wrap"><div class="card"><h2>AgentNexus is unreachable</h2><p class="hint">${esc(err.message)}</p></div></div>`;
  }
}

window.addEventListener('hashchange', () => route());
boot();
