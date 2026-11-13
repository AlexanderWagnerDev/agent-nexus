/* Formatting and agent metadata helpers shared by the browser views. */

import { t, getLang, tp } from './i18n.js';
import { svgIcon } from './icons.js';

export function esc(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

export function uiLocale() {
  return getLang() === 'de' ? 'de-DE' : 'en-US';
}

export function fmtTime(value) {
  if (!value) return '—';
  const d = new Date(value);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1970) return '—';
  return d.toLocaleString(uiLocale(), { dateStyle: 'medium', timeStyle: 'short' });
}

export function fmtStamp(value) {
  if (!value) return '';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '';
  const now = new Date();
  const time = d.toLocaleTimeString(uiLocale(), { hour: '2-digit', minute: '2-digit' });
  if (d.toDateString() === now.toDateString()) return time;
  return `${d.toLocaleDateString(uiLocale(), { month: 'short', day: 'numeric' })}, ${time}`;
}

export function fmtRelative(value) {
  if (!value) return '—';
  const d = new Date(value);
  const diff = Date.now() - d.getTime();
  if (Number.isNaN(diff)) return '—';
  const s = Math.round(diff / 1000);
  if (s < 45) return t('just now');
  const m = Math.round(s / 60);
  if (m < 60) return `${m}${t('m ago')}`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}${t('h ago')}`;
  const days = Math.round(h / 24);
  if (days < 30) return `${days}${t('d ago')}`;
  return new Date(value).toLocaleDateString(uiLocale());
}

export function shortId(id) {
  return id ? id.slice(0, 10) : '';
}

export function countLabel(count, singular, plural) {
  return tp(count, singular, plural);
}

export const AGENTS = {
  opencode: { label: 'OpenCode', short: 'OC' },
  codex: { label: 'Codex', short: 'CX' },
  claude: { label: 'Claude Code', short: 'CL' },
  gemini: { label: 'Gemini', short: 'GM' },
  copilot: { label: 'Copilot', short: 'CP' },
  goose: { label: 'Goose', short: 'GO' },
};

export function agentMeta(agent) {
  return AGENTS[String(agent || '').toLowerCase()] || { label: agent || 'Agent', short: String(agent || '?').slice(0, 2).toUpperCase() };
}

export function agentVersion(name, version) {
  const value = String(version || '').trim();
  if (!value) return '';
  return value.replace(new RegExp(`^${String(name || '').replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}[\\s-]*(cli[\\s-]*)?`, 'i'), '') || value;
}

export function projectName(cwd) {
  const clean = String(cwd || '').replaceAll('\\', '/').replace(/\/+$/, '');
  if (!clean) return 'No project';
  const parts = clean.split('/');
  return parts[parts.length - 1] || clean;
}

export function cwdKey(cwd) {
  return String(cwd || '').replaceAll('\\', '/').replace(/\/+$/, '').toLowerCase();
}

/** Lowercase a git remote so the same repository on several hosts collapses. */
export function normalizeRepoURL(url) {
  const value = String(url || '').trim();
  if (!value) return '';
  return value.replace(/\/+$/, '').replace(/\.git$/i, '').toLowerCase();
}

/**
 * Group project rows by normalized remote URL. Worktrees without a remote fall
 * back to host + path, so unrelated local projects never merge. Used by Home,
 * the sidebar and the Projects page so all three show one row per repository.
 */
export function groupProjects(projects) {
  const groups = new Map();
  for (const project of projects || []) {
    const remote = normalizeRepoURL(project.repo_url);
    const key = remote ? `url:${remote}` : `path:${project.host_id}:${cwdKey(project.cwd)}`;
    let group = groups.get(key);
    if (!group) {
      group = {
        key, url: project.repo_url || '', name: projectName(project.cwd),
        cwd: project.cwd, host_id: project.host_id,
        projects: [], hosts: [], branches: [], agents: [],
        sessions: 0, active: 0, lastAt: project.last_at,
      };
      groups.set(key, group);
    }
    group.projects.push(project);
    if (!group.hosts.some((host) => host.id === project.host_id)) {
      group.hosts.push({ id: project.host_id, name: project.host_name || project.host_id });
    }
    if (project.branch && !group.branches.includes(project.branch)) group.branches.push(project.branch);
    for (const agent of project.agents || []) if (!group.agents.includes(agent)) group.agents.push(agent);
    group.sessions += project.sessions || 0;
    group.active += project.active || 0;
    if (new Date(project.last_at) > new Date(group.lastAt)) group.lastAt = project.last_at;
  }
  return [...groups.values()].sort((a, b) => new Date(b.lastAt) - new Date(a.lastAt));
}

// The host detects a native terminal session by scanning running process
// command lines. That scan is best effort, so the badge explains the
// uncertainty instead of presenting it as hard proof.
export function nativeActivityBadge(active, compact = false) {
  if (!active) return '';
  const hint = t('Best-effort detection: an agent process appears to have this session open. Confirm in the native terminal before resuming.');
  return `<span class="badge active_elsewhere${compact ? ' compact' : ''}" title="${esc(hint)}">${t('Open in native terminal')}</span>`;
}

export function repoLink(url, label = '') {
  const value = String(url || '');
  if (!/^https?:\/\//i.test(value)) return '';
  const name = label || value.replace(/^https?:\/\/[^/]+\//, '');
  return `<a class="repo-link" href="${esc(value)}" target="_blank" rel="noopener noreferrer">${svgIcon('git', 13)}${esc(name)}</a>`;
}

export function fmtDuration(value) {
  if (!value) return '—';
  const ms = typeof value === 'number' ? value : Date.now() - new Date(value).getTime();
  if (!Number.isFinite(ms) || ms < 0) return '—';
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}
