import assert from 'node:assert/strict';
import test from 'node:test';

import { agentLogo } from '../assets/icons.js';

test('agentLogo renders known agents', () => {
  const svg = agentLogo('opencode');
  assert.match(svg, /^<svg class="agent-logo"/);
});

test('agentLogo ignores inherited prototype keys', () => {
  assert.equal(agentLogo('__proto__'), '');
  assert.equal(agentLogo('constructor'), '');
  assert.equal(agentLogo('toString'), '');
});

test('agentLogo falls back for unknown names', () => {
  assert.equal(agentLogo('unknown-agent'), '');
  assert.equal(agentLogo(''), '');
});
