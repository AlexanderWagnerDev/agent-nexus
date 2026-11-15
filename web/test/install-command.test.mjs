import test from 'node:test';
import assert from 'node:assert/strict';
import { installCommand, installScriptRef } from '../assets/install-command.js';

test('pins release installers to the tag and development builds to main', () => {
  assert.equal(installScriptRef('0.1.1'), 'v0.1.1');
  assert.equal(installScriptRef('v0.1.1'), 'v0.1.1');
  assert.equal(installScriptRef('0.1.1-rc.1'), 'v0.1.1-rc.1');
  assert.equal(installScriptRef('alpha-abc1234'), 'main');
  assert.equal(installScriptRef(''), 'main');
});

test('builds a shell one-liner for linux and macos', () => {
  for (const os of ['linux', 'macos']) {
    const command = installCommand(os, 'https://nexus.example.com', 'ABCD-1234', '0.1.1');
    assert.ok(command.startsWith('curl -fsSL https://raw.githubusercontent.com/AlexanderWagnerDev/agent-nexus/v0.1.1/scripts/install-host.sh | bash -s --'));
    assert.ok(command.includes("--hub 'https://nexus.example.com' --code 'ABCD-1234'"));
  }
});

test('builds a Windows one-liner with PowerShell parameters', () => {
  const command = installCommand('windows', 'https://nexus.example.com', 'ABCD-1234', '0.1.1');
  assert.ok(command.startsWith('powershell -NoProfile -ExecutionPolicy Bypass -Command "'));
  assert.ok(command.includes("install-host.ps1' -OutFile $env:TEMP\\agentnexus-install.ps1"));
  assert.ok(command.includes("-Hub 'https://nexus.example.com' -Code 'ABCD-1234'"));
});

test('quotes values for the target shell', () => {
  assert.ok(installCommand('linux', "https://x/'y", "a'b", '0.1.1').includes("--hub 'https://x/'\\''y' --code 'a'\\''b'"));
  assert.ok(installCommand('windows', "https://x/'y", "a'b", '0.1.1').includes("-Hub 'https://x/''y' -Code 'a''b'"));
});
