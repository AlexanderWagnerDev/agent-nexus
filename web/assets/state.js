/* Shared browser state and local storage helpers. */
export const state = {
  me: null,
  setupRequired: false,
  hubName: 'AgentNexus',
  version: '',
  embed: false,
  settings: { theme: 'system' },
  ws: null,
  wsRetry: 1000,
  wsGen: 0,
  wsHandlers: new Set(),
  pageCleanup: null,
  pageGen: 0,
  run: null,
  favorites: new Set(),
  notifications: [],
  unread: 0,
  notifFilter: 'all',
  openTabs: [],
  palette: null,
  notifTimer: null,
};

/** Parse a stored JSON value, falling back on missing or corrupt data. */
export function parseStored(raw, fallback) {
  try { return JSON.parse(raw) ?? fallback; } catch { return fallback; }
}

export function storeGet(key, fallback) {
  try { return parseStored(localStorage.getItem(key), fallback); } catch { return fallback; }
}

export function storeSet(key, value) {
  try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* private mode */ }
}

export function draftKey(runID) {
  return `nexus-draft-${runID}`;
}

// hasSavedAccountArtifacts reports whether this browser holds tabs, recent
// command-palette entries, prompt drafts or run configuration.
export function hasSavedAccountArtifacts() {
  try {
    if (localStorage.getItem('nexus-tabs') || localStorage.getItem('nexus-recent')) return true;
    return Object.keys(localStorage).some((key) => key.startsWith('nexus-draft-') || key.startsWith('nexus-run-config-'));
  } catch {
    return false;
  }
}

// shouldClearAccountArtifacts decides whether stored artifacts belong to the
// account that is signing in. With no recorded owner, any saved artifact is
// unowned and must be cleared instead of being shown to the next account.
export function shouldClearAccountArtifacts(owner, userID) {
  if (!userID) return false;
  return owner ? owner !== userID : hasSavedAccountArtifacts();
}
