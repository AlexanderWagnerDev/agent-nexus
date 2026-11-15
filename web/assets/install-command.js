/* Install-and-pair one-liners shown by the Pair-a-host dialog. */

const RAW_BASE = 'https://raw.githubusercontent.com/AlexanderWagnerDev/agent-nexus';

/** Release builds pin the installer to their tag; development builds use main. */
export function installScriptRef(version) {
  const value = String(version || '').trim().replace(/^v/, '');
  return /^\d+\.\d+\.\d+/.test(value) ? `v${value}` : 'main';
}

function shellQuote(value) {
  return `'${String(value).replaceAll("'", "'\\''")}'`;
}

function psQuote(value) {
  return `'${String(value).replaceAll("'", "''")}'`;
}

/** Command that installs nexusd, pairs it with the hub and installs the service. */
export function installCommand(os, hubUrl, code, version) {
  const base = `${RAW_BASE}/${installScriptRef(version)}/scripts`;
  if (os === 'windows') {
    const inner = `iwr -UseBasicParsing ${psQuote(`${base}/install-host.ps1`)} -OutFile $env:TEMP\\agentnexus-install.ps1; & $env:TEMP\\agentnexus-install.ps1 -Hub ${psQuote(hubUrl)} -Code ${psQuote(code)}`;
    return `powershell -NoProfile -ExecutionPolicy Bypass -Command "${inner}"`;
  }
  return `curl -fsSL ${base}/install-host.sh | bash -s -- --hub ${shellQuote(hubUrl)} --code ${shellQuote(code)}`;
}
