/* Tool-call normalisation for compact chat cards. */

export function parseToolInput(input) {
  if (input && typeof input === 'object') return input;
  const text = String(input ?? '').trim();
  if (!text) return null;
  if (!/^[\[{]/.test(text)) return null;
  try { return JSON.parse(text); } catch { return null; }
}

export function stripAnsi(value) {
  return String(value ?? '').replace(/\u001b\[[0-9;?]*[ -/]*[@-~]/g, '');
}

function firstString(...values) {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value.trim();
  }
  return '';
}

function commandText(obj) {
  if (!obj) return '';
  if (typeof obj.command === 'string') return obj.command;
  if (Array.isArray(obj.command)) return obj.command.join(' ');
  if (typeof obj.cmd === 'string') return obj.cmd;
  if (Array.isArray(obj.args) && obj.args.length) return obj.args.join(' ');
  return '';
}

function filePath(obj) {
  if (!obj) return '';
  return firstString(obj.filePath, obj.file_path, obj.path, obj.file, obj.filename);
}

function changePaths(obj) {
  if (!obj || !Array.isArray(obj.changes)) return [];
  return obj.changes.map((change) => firstString(change.path, change.filePath, change.file)).filter(Boolean);
}

export function classifyTool(name, obj) {
  const raw = `${name || ''} ${obj?.type || ''}`.toLowerCase();
  if (/filechange|apply_patch|edit|write|patch/.test(raw)) return /write/.test(raw) ? 'write' : 'edit';
  if (/commandexecution|bash|shell|command|exec/.test(raw)) return 'terminal';
  if (/read/.test(raw)) return 'read';
  if (/grep|glob|search|find/.test(raw)) return 'search';
  if (/webfetch|websearch|browser|fetch/.test(raw)) return 'web';
  if (/git/.test(raw)) return 'git';
  if (/test/.test(raw)) return 'test';
  if (/todo|plan/.test(raw)) return 'todo';
  if (/task|subagent/.test(raw)) return 'task';
  if (/mcp/.test(raw)) return 'mcp';
  if (/image|screenshot/.test(raw)) return 'image';
  return 'tool';
}

const LABELS = {
  terminal: 'Ran command',
  read: 'Read file',
  edit: 'Edited file',
  write: 'Wrote file',
  search: 'Searched files',
  web: 'Searched the web',
  git: 'Checked git',
  test: 'Ran tests',
  todo: 'Updated plan',
  task: 'Delegated task',
  mcp: 'Called MCP tool',
  image: 'Viewed image',
  tool: 'Used tool',
};

function buildDiff(obj) {
  if (!obj) return '';
  if (typeof obj.diff === 'string' && obj.diff.trim()) return obj.diff;
  if (Array.isArray(obj.changes)) {
    return obj.changes.map((change) => {
      const path = firstString(change.path, change.filePath, change.file) || 'file';
      if (typeof change.diff === 'string' && change.diff.trim()) {
        return change.diff.startsWith('diff --git') ? change.diff : `diff --git a/${path} b/${path}\n${change.diff}`;
      }
      if (typeof change.content === 'string') {
        return `diff --git a/${path} b/${path}\n--- a/${path}\n+++ b/${path}\n${change.content.split('\n').map((line) => `+${line}`).join('\n')}`;
      }
      return '';
    }).filter(Boolean).join('\n');
  }
  const path = filePath(obj);
  const before = typeof obj.oldString === 'string' ? obj.oldString : (typeof obj.old_string === 'string' ? obj.old_string : '');
  const after = typeof obj.newString === 'string' ? obj.newString : (typeof obj.new_string === 'string' ? obj.new_string : '');
  if (path && (before || after)) {
    const minus = before ? before.split('\n').map((line) => `-${line}`).join('\n') : '';
    const plus = after ? after.split('\n').map((line) => `+${line}`).join('\n') : '';
    return `diff --git a/${path} b/${path}\n--- a/${path}\n+++ b/${path}\n${minus}${minus && plus ? '\n' : ''}${plus}`;
  }
  return '';
}

/** Convert a raw tool call into the compact data used by the chat cards. */
export function describeTool(name, input, output = '') {
  const obj = parseToolInput(input);
  const kind = classifyTool(name, obj);
  const command = commandText(obj);
  const path = filePath(obj);
  const paths = changePaths(obj);
  const pattern = firstString(obj?.pattern, obj?.query, obj?.glob, obj?.include);
  const description = firstString(obj?.description, obj?.summary, obj?.title);
  const mcp = obj && (obj.server || obj.tool) ? [obj.server, obj.tool].filter(Boolean).join(' / ') : '';
  let detail = '';
  if (kind === 'terminal' || kind === 'git' || kind === 'test') detail = command || description;
  else if (kind === 'edit' || kind === 'write') detail = paths.length > 1 ? `${paths.length} files` : (paths[0] || path || description);
  else if (kind === 'read') detail = path || description;
  else if (kind === 'search') detail = pattern || description;
  else if (kind === 'web') detail = firstString(obj?.url, obj?.query, description);
  else if (kind === 'mcp') detail = mcp || description;
  else if (kind === 'task') detail = description || firstString(obj?.prompt);
  else if (kind === 'todo' && Array.isArray(obj?.todos)) detail = `${obj.todos.length} items`;
  else detail = path || command || pattern || description;
  if (!detail && !obj && typeof input === 'string') detail = input.trim().split('\n')[0].slice(0, 160);
  const rawOutput = stripAnsi(output);
  const diff = buildDiff(obj) || (/^(diff --git|@@ )/m.test(rawOutput) ? rawOutput : '');
  return {
    kind,
    label: LABELS[kind] || LABELS.tool,
    detail: String(detail || '').replace(/\s+/g, ' ').trim().slice(0, 180),
    command,
    path,
    paths,
    diff,
    inputText: typeof input === 'string' ? input : JSON.stringify(input ?? '', null, 2),
    output: rawOutput,
  };
}

export function toolStatusLabel(status) {
  if (status === 'running') return 'Running';
  if (status === 'error') return 'Failed';
  return 'Done';
}
