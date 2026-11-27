import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const chat = fs.readFileSync(path.join(root, 'web/assets/views/chat.js'), 'utf8');

// A structured run started from the browser has no session id until the host
// reports the native session. The transcript loader used to return early in
// that case, leaving every live event buffered forever, so a new chat showed
// the prompt and nothing else.
test('the transcript loader cannot skip the live-event flush', () => {
  assert.ok(!chat.includes('if (!run.session_id) return;'),
    'chat.js must not return before historyReady is set');
  const ready = chat.indexOf('historyReady = true;');
  const flush = chat.indexOf('flushPendingLiveEvents();', ready);
  assert.ok(ready > 0, 'historyReady is never set');
  assert.ok(flush > ready, 'buffered live events are never flushed');
});

// OpenCode TUI writes bypass hub runs, so the chat only learns about them from
// the host's session delta or the transcript poll.
test('an open chat follows writes made in the native CLI', () => {
  assert.ok(chat.includes("msg.type === 'sessions.changed'"), 'chat does not listen for native session changes');
  assert.ok(chat.includes('clearInterval(transcriptTimer)'), 'the transcript poll is not cleared with the page');
});
