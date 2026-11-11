import test from 'node:test';
import assert from 'node:assert/strict';
import { t, setLang } from '../assets/i18n.js';

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
