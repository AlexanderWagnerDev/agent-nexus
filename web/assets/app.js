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
import { renderAuth, renderDevicePair, ensureStepUp, clearAccountArtifacts, adoptAccountArtifacts } from './auth.js';
import { pageOverview } from './views/overview.js';
import { pageHosts } from './views/hosts.js';
import { pageHostDetail } from './views/host-detail.js';
import { pageProjects } from './views/projects.js';
import { pageSessions } from './views/sessions.js';
import { pageTasks } from './views/tasks.js';
import { pageUsage } from './views/usage.js';
import { pageFanout } from './views/fanout.js';
import { pageSession, pageSessionChanges } from './views/session-detail.js';
import { pageSearch } from './views/search.js';
import { pageSettings } from './views/settings.js';
import { pageDevices } from './views/devices.js';
import { pageUsers } from './views/users.js';
import { pageAudit } from './views/audit.js';
import { pageDiagnostics } from './views/diagnostics.js';
import { pageRuns } from './views/runs.js';
import { pageWorkspace } from './views/workspace.js';
import { pageRun } from './views/run.js';

initLang();
setUnauthorizedHook(() => {
  clearAccountArtifacts();
  renderAuth('login');
});
setStepUpHook(() => ensureStepUp());

const pages = {
  overview: pageOverview,
  hosts: pageHosts,
  host: pageHostDetail,
  projects: pageProjects,
  sessions: pageSessions,
  tasks: pageTasks,
  usage: pageUsage,
  fanout: pageFanout,
  session: pageSession,
  sessionChanges: pageSessionChanges,
  search: pageSearch,
  settings: pageSettings,
  devices: pageDevices,
  users: pageUsers,
  audit: pageAudit,
  diagnostics: pageDiagnostics,
  runs: pageRuns,
  workspace: pageWorkspace,
  run: pageRun,
};

setPageHandler(async () => {
  const target = parseRoute(location.hash);
  // Preferences are user-scoped and may have changed on another device; pick
  // them up before rendering the next page instead of only at sign-in.
  await loadUserTheme();
  return await pages[target.page](target.id);
});
async function boot() {
  state.embed = new URLSearchParams(location.search).get('embed') === '1';
  if (state.embed) document.documentElement.dataset.embed = '1';
  installGlobalUi();
  if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {});
  let pairToken = new URLSearchParams(location.search).get('pair');
  if (!pairToken) {
    try { pairToken = sessionStorage.getItem('nexus-pair-token'); } catch { /* storage unavailable */ }
  }
  if (pairToken) return renderDevicePair(pairToken);
  try {
    const status = await api('/setup/status', { allow401: true });
    state.setupRequired = status.setup_required;
    state.hubName = status.hub_name || state.hubName;
    state.version = status.version || state.version;
    if (status.setup_required) return renderAuth('setup');
    try {
      state.me = await api('/me', { allow401: true });
      adoptAccountArtifacts(state.me.id);
      await loadUserTheme();
      renderShell('overview');
      await route();
    } catch (err) {
      // /me is called with allow401 so the shared hook does not run; a real
      // 401 still ends the session and must clear the previous account's data.
      // A 5xx or network error keeps the drafts for the next successful load.
      if (err?.status === 401) clearAccountArtifacts();
      renderAuth('login');
    }
  } catch (err) {
    app.innerHTML = `<div class="auth-wrap"><div class="card"><h2>AgentNexus is unreachable</h2><p class="hint">${esc(err.message)}</p></div></div>`;
  }
}

window.addEventListener('hashchange', () => route());
boot();
