/* Pure hash-to-page resolution, shared by the router and its unit tests. */

/** Map a hash to the page name and its path id. Pure and unit-tested. */
export function parseRoute(hash) {
  const h = hash || '#/';
  if (h.startsWith('#/hosts/')) return { page: 'host', id: h.split('/')[2] };
  if (h.startsWith('#/hosts')) return { page: 'hosts' };
  if (h.startsWith('#/projects')) return { page: 'projects' };
  if (h.startsWith('#/sessions/') && h.endsWith('/changes')) return { page: 'sessionChanges', id: h.split('/')[2] };
  if (h.startsWith('#/sessions/')) return { page: 'session', id: h.split('/')[2] };
  if (h.startsWith('#/sessions')) return { page: 'sessions' };
  if (h.startsWith('#/tasks')) return { page: 'tasks' };
  if (h.startsWith('#/usage')) return { page: 'usage' };
  if (h.startsWith('#/fanout')) return { page: 'fanout' };
  if (h === '#/runs') return { page: 'runs' };
  if (h.startsWith('#/workspace')) return { page: 'workspace' };
  if (h.startsWith('#/runs/')) return { page: 'run', id: h.split('/')[2] };
  if (h.startsWith('#/search')) return { page: 'search' };
  if (h.startsWith('#/settings')) return { page: 'settings' };
  if (h.startsWith('#/devices')) return { page: 'devices' };
  if (h.startsWith('#/users')) return { page: 'users' };
  if (h.startsWith('#/audit')) return { page: 'audit' };
  if (h.startsWith('#/diagnostics')) return { page: 'diagnostics' };
  return { page: 'overview' };
}

export function activeFromHash() {
  const h = location.hash || '#/';
  if (h.startsWith('#/hosts')) return 'hosts';
  if (h.startsWith('#/projects')) return 'projects';
  if (h.startsWith('#/sessions')) return 'sessions';
  if (h.startsWith('#/tasks')) return 'tasks';
  if (h.startsWith('#/usage')) return 'usage';
  if (h.startsWith('#/fanout')) return 'fanout';
  if (h.startsWith('#/runs')) return 'runs';
  if (h.startsWith('#/workspace')) return 'workspace';
  if (h.startsWith('#/search')) return 'search';
  if (h.startsWith('#/settings')) return 'settings';
  if (h.startsWith('#/devices') || h.startsWith('#/users') || h.startsWith('#/audit')) return 'settings';
  if (h.startsWith('#/diagnostics')) return 'settings';
  return 'overview';
}
