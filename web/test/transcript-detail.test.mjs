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
  reasoningHtml, renderTranscriptMessage, renderTranscriptMessages, setTranscriptDetail, toolCardHtml, transcriptDetail,
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

test('chat level hides reasoning and tool calls in history rendering', () => {
  setTranscriptDetail('chat');
  assert.equal(renderTranscriptMessages([
    { kind: 'tool_call', tool_name: 'bash', tool_input: '{}' },
    { kind: 'tool_result', tool_output: 'ok' },
  ]), '');
});

test('chat level hides reasoning and tool calls', () => {
  setTranscriptDetail('chat');
  assert.equal(renderTranscriptMessage({ kind: 'reasoning', text: 'thinking' }), '');
  assert.equal(renderTranscriptMessage({ kind: 'tool_call', tool_name: 'bash', tool_input: '{}' }), '');
});

test('full level expands reasoning and tool cards', () => {
  setTranscriptDetail('full');
  assert.match(reasoningHtml('thinking'), /<details class="reasoning-card" open>/);
  assert.match(toolCardHtml({ label: 'Run echo', kind: 'terminal' }, 'done'), /<details class="tool-card done" open>/);
});

test('synthetic agent prompts are only shown at full detail', () => {
  const message = { role: 'user', text: '<environment_context>hidden</environment_context>' };
  setTranscriptDetail('normal');
  assert.equal(renderTranscriptMessage(message), '');
  setTranscriptDetail('full');
  assert.match(renderTranscriptMessage(message), /environment_context/);
});
