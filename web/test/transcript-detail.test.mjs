import test from 'node:test';
import assert from 'node:assert/strict';

// The browser module reads and writes the transcript detail preference through
// localStorage; Node has no storage, so provide a tiny in-memory replacement.
const memory = new Map();
globalThis.localStorage = {
  getItem: (key) => (memory.has(key) ? memory.get(key) : null),
  setItem: (key, value) => memory.set(key, String(value)),
};

const { state } = await import('../assets/state.js');
const {
  reasoningHtml, renderTranscriptMessage, renderTranscriptMessages, setTranscriptDetail, toolCardHtml,
  transcriptDetail, transcriptDetailOptions,
} = await import('../assets/components/transcript.js');

test('defaults to normal and rejects unknown levels', () => {
  assert.equal(transcriptDetail(), 'normal');
  setTranscriptDetail('neon');
  assert.equal(transcriptDetail(), 'normal');
});

test('hub preference wins over the device cache', () => {
  setTranscriptDetail('chat');
  state.settings = { ...state.settings, transcript_detail: 'full' };
  assert.equal(transcriptDetail(), 'full');
  state.settings = { ...state.settings, transcript_detail: undefined };
});

test('an unset hub preference keeps the device choice', () => {
  setTranscriptDetail('full');
  state.settings = { ...state.settings, transcript_detail: '' };
  assert.equal(transcriptDetail(), 'full');
  state.settings = { ...state.settings, transcript_detail: undefined };
});

test('chat level keeps tool rows in the DOM but hidden', () => {
  setTranscriptDetail('chat');
  const html = renderTranscriptMessages([{ kind: 'tool_call', tool_name: 'bash', tool_input: '{}' }]);
  assert.match(html, /<details class="tool-card done" hidden/);
});

test('chat level keeps reasoning and tool calls hidden instead of dropping them', () => {
  setTranscriptDetail('chat');
  assert.match(renderTranscriptMessage({ kind: 'reasoning', text: 'thinking' }), /turn-reasoning" hidden/);
  assert.match(renderTranscriptMessage({ kind: 'tool_call', tool_name: 'bash', tool_input: '{}' }), /tool-card done" hidden/);
});

test('full level expands reasoning and tool cards', () => {
  setTranscriptDetail('full');
  assert.match(reasoningHtml('thinking'), /<details class="reasoning-card" open>/);
  assert.match(toolCardHtml({ label: 'Run echo', kind: 'terminal' }, 'done'), /<details class="tool-card done" open>/);
});

test('the inline selector reflects the active level', () => {
  setTranscriptDetail('full');
  assert.match(transcriptDetailOptions(), /<option value="full" selected>/);
  assert.doesNotMatch(transcriptDetailOptions(), /<option value="chat" selected>/);
});

test('synthetic agent prompts are only shown at full detail', () => {
  const message = { role: 'user', text: '<environment_context>hidden</environment_context>' };
  setTranscriptDetail('normal');
  assert.equal(renderTranscriptMessage(message), '');
  setTranscriptDetail('full');
  assert.match(renderTranscriptMessage(message), /environment_context/);
});
