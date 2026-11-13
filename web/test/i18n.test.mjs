import test from 'node:test';
import assert from 'node:assert/strict';
import { t, setLang, pluralNoun, tp } from '../assets/i18n.js';
import { countLabel } from '../assets/ui-format.js';

test('falls back to English and interpolates', () => {
  setLang('en');
  assert.equal(t('Home'), 'Home');
  assert.equal(t('{n} sessions', { n: 2 }), '2 sessions');
  assert.equal(t('Missing key'), 'Missing key');
});

test('translates German shell and counts', () => {
  setLang('de');
  assert.equal(t('Home'), 'Start');
  assert.equal(t('Settings'), 'Einstellungen');
  assert.equal(t('session'), 'Session');
  assert.equal(t('sessions'), 'Sessions');
  assert.equal(t('Message {agent}…', { agent: 'Codex' }), 'Nachricht an Codex…');
});

test('unknown language falls back to English', () => {
  setLang('fr');
  assert.equal(t('Home'), 'Home');
  setLang('en');
});

test('uses Intl.PluralRules for English and German counts', () => {
  setLang('en');
  assert.equal(tp(0, 'host', 'hosts'), '0 hosts');
  assert.equal(tp(1, 'host', 'hosts'), '1 host');
  assert.equal(tp(2, 'host', 'hosts'), '2 hosts');
  assert.equal(countLabel(1, 'open run', 'open runs'), '1 open run');
  assert.equal(countLabel(3, 'open run', 'open runs'), '3 open runs');
  setLang('de');
  assert.equal(pluralNoun(1, 'host', 'hosts'), 'Host');
  assert.equal(pluralNoun(2, 'host', 'hosts'), 'Hosts');
  assert.equal(countLabel(1, 'session', 'sessions'), '1 Session');
  assert.equal(countLabel(2, 'session', 'sessions'), '2 Sessions');
  setLang('en');
});
