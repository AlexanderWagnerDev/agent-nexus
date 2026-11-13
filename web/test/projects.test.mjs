import test from 'node:test';
import assert from 'node:assert/strict';
import { groupProjects, normalizeRepoURL } from '../assets/ui-format.js';
import { parseDiffStat } from '../assets/chat-format.js';

test('normalizes git remotes for cross-host grouping', () => {
  assert.equal(normalizeRepoURL('https://github.com/Example/Sample-API.git/'), 'https://github.com/example/sample-api');
  assert.equal(normalizeRepoURL(''), '');
});

test('groups identical remotes across hosts into one project', () => {
  const groups = groupProjects([
    { host_id: 'h1', host_name: 'linux-builder', cwd: '/srv/sample-api', repo_url: 'https://github.com/example/sample-api', sessions: 2, active: 1, agents: ['claude'], branch: 'main', last_at: '2026-11-13T10:00:00Z' },
    { host_id: 'h2', host_name: 'windows-dev', cwd: 'C:/work/sample-api', repo_url: 'https://github.com/example/sample-api.git', sessions: 3, active: 0, agents: ['codex'], branch: 'feat/x', last_at: '2026-11-13T11:00:00Z' },
    { host_id: 'h3', host_name: 'macbook-demo', cwd: '/Users/demo/other', repo_url: '', sessions: 1, last_at: '2026-11-12T10:00:00Z' },
  ]);
  assert.equal(groups.length, 2);
  const shared = groups.find((group) => group.url);
  assert.deepEqual(shared.hosts.map((host) => host.name), ['linux-builder', 'windows-dev']);
  assert.equal(shared.sessions, 5);
  assert.equal(shared.active, 1);
  assert.deepEqual(shared.agents, ['claude', 'codex']);
});

test('parses a diffstat into file rows with additions and deletions', () => {
  const stat = parseDiffStat(' internal/http/retry.go      | 7 ++++---\n internal/http/retry_test.go | 8 ++++++++\n 2 files changed, 12 insertions(+), 3 deletions(-)');
  assert.equal(stat.rows.length, 2);
  assert.deepEqual(stat.rows[0], { file: 'internal/http/retry.go', changes: 7, additions: 4, deletions: 3 });
  assert.equal(stat.rows[1].additions, 8);
  assert.match(stat.summary, /2 files changed/);
  assert.equal(parseDiffStat(''), null);
});
