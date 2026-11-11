/* Unified/split diff parsing and rendering for the AgentNexus review panel. */

import { escapeHtml, highlightCode } from './markdown.js';

function cleanPath(value) {
  let path = String(value || '').trim();
  if (path.startsWith('a/') || path.startsWith('b/')) path = path.slice(2);
  if (path === '/dev/null') return '';
  return path;
}

function languageFor(path) {
  const ext = String(path || '').split('.').pop().toLowerCase();
  return {
    js: 'javascript', jsx: 'javascript', mjs: 'javascript', cjs: 'javascript',
    ts: 'typescript', tsx: 'typescript', py: 'python', rb: 'ruby',
    go: 'go', rs: 'rust', java: 'java', kt: 'java', kts: 'java',
    cs: 'csharp', cpp: 'cpp', cc: 'cpp', c: 'c', h: 'c', hpp: 'cpp',
    sh: 'bash', bash: 'bash', zsh: 'bash', ps1: 'powershell',
    json: 'json', jsonc: 'json', yml: 'yaml', yaml: 'yaml',
    toml: 'toml', sql: 'sql', css: 'css', scss: 'css', html: 'html',
    md: 'markdown', xml: 'xml', php: 'php', swift: 'swift',
  }[ext] || '';
}

/** Parse git/tool unified diff output into files and hunks. */
export function parseUnifiedDiff(source) {
  const text = String(source || '').replace(/\r\n?/g, '\n');
  if (!text.trim()) return [];
  const files = [];
  let file = null;
  let hunk = null;
  let oldLine = 0;
  let newLine = 0;
  const ensureFile = () => {
    if (file) return file;
    file = { path: 'changes', oldPath: '', newPath: '', additions: 0, deletions: 0, hunks: [], binary: false };
    files.push(file);
    return file;
  };
  for (const line of text.split('\n')) {
    if (line.startsWith('diff --git ')) {
      const match = line.match(/^diff --git a\/(.*?) b\/(.*)$/);
      file = { path: cleanPath(match?.[2] || match?.[1] || 'changes'), oldPath: cleanPath(match?.[1]), newPath: cleanPath(match?.[2]), additions: 0, deletions: 0, hunks: [], binary: false };
      files.push(file);
      hunk = null;
      continue;
    }
    if (line.startsWith('--- ')) {
      ensureFile().oldPath = cleanPath(line.slice(4));
      continue;
    }
    if (line.startsWith('+++ ')) {
      ensureFile().newPath = cleanPath(line.slice(4));
      if (file?.newPath) file.path = file.newPath;
      continue;
    }
    const hunkMatch = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$/);
    if (hunkMatch) {
      const target = ensureFile();
      oldLine = Number(hunkMatch[1]);
      newLine = Number(hunkMatch[2]);
      hunk = { header: line, oldStart: oldLine, newStart: newLine, lines: [] };
      target.hunks.push(hunk);
      continue;
    }
    if (!hunk) {
      if (/^Binary files .* differ$/.test(line)) ensureFile().binary = true;
      continue;
    }
    if (line.startsWith('\\ No newline at end of file')) {
      hunk.lines.push({ type: 'meta', text: line, oldLine: null, newLine: null });
      continue;
    }
    const first = line[0];
    if (first === '+' && !line.startsWith('+++')) {
      hunk.lines.push({ type: 'add', text: line.slice(1), oldLine: null, newLine });
      file.additions += 1;
      newLine += 1;
    } else if (first === '-' && !line.startsWith('---')) {
      hunk.lines.push({ type: 'del', text: line.slice(1), oldLine, newLine: null });
      file.deletions += 1;
      oldLine += 1;
    } else if (first === ' ') {
      hunk.lines.push({ type: 'context', text: line.slice(1), oldLine, newLine });
      oldLine += 1;
      newLine += 1;
    } else if (line === '' && hunk.lines.length) {
      hunk.lines.push({ type: 'context', text: '', oldLine, newLine });
      oldLine += 1;
      newLine += 1;
    }
  }
  return files.filter((entry) => entry.hunks.length || entry.binary);
}

function renderLineNumbers(oldLine, newLine) {
  return `<span class="diff-ln">${oldLine ?? ''}</span><span class="diff-ln">${newLine ?? ''}</span>`;
}

function renderCode(text, language) {
  return `<code>${highlightCode(text, language)}</code>`;
}

export function renderUnifiedDiff(files, options = {}) {
  return files.map((file) => {
    const language = languageFor(file.path);
    const hunks = file.binary
      ? '<div class="diff-binary">Binary file differs</div>'
      : file.hunks.map((hunk) => `
        <div class="diff-hunk-block">
          <div class="diff-hunk-title">${escapeHtml(hunk.header)}</div>
          ${hunk.lines.map((line) => `
            <div class="diff-line diff-row-${line.type}">
              ${renderLineNumbers(line.oldLine, line.newLine)}
              <span class="diff-marker">${line.type === 'add' ? '+' : line.type === 'del' ? '-' : line.type === 'meta' ? '\\' : ''}</span>
              ${renderCode(line.text, language)}
            </div>`).join('')}
        </div>`).join('');
    return `<section class="diff-file-section" data-diff-file="${escapeHtml(file.path)}">
      <button class="diff-file-head" type="button" data-diff-jump="${escapeHtml(file.path)}">
        ${escapeHtml(file.path)}
        <span class="diff-file-stat"><b>+${file.additions}</b><i>-${file.deletions}</i></span>
      </button>
      <div class="diff-file-body">${hunks}</div>
    </section>`;
  }).join('');
}

function rowsForHunk(hunk) {
  const rows = [];
  let index = 0;
  while (index < hunk.lines.length) {
    const line = hunk.lines[index];
    if (line.type === 'context' || line.type === 'meta') {
      rows.push({ left: line.type === 'context' ? line : null, right: line.type === 'context' ? line : null, meta: line.type === 'meta' ? line : null });
      index += 1;
      continue;
    }
    const dels = [];
    const adds = [];
    while (index < hunk.lines.length && hunk.lines[index].type === 'del') dels.push(hunk.lines[index++]);
    while (index < hunk.lines.length && hunk.lines[index].type === 'add') adds.push(hunk.lines[index++]);
    const count = Math.max(dels.length, adds.length);
    for (let i = 0; i < count; i += 1) rows.push({ left: dels[i] || null, right: adds[i] || null });
  }
  return rows;
}

export function renderSplitDiff(files) {
  return files.map((file) => {
    const language = languageFor(file.path);
    const hunks = file.binary
      ? '<div class="diff-binary">Binary file differs</div>'
      : file.hunks.map((hunk) => {
        const rows = rowsForHunk(hunk).map((row) => {
          if (row.meta) return `<div class="diff-split-meta">${escapeHtml(row.meta.text)}</div>`;
          const left = row.left
            ? `<div class="diff-split-cell diff-cell-del"><span class="diff-ln">${row.left.oldLine ?? ''}</span>${renderCode(row.left.text, language)}</div>`
            : '<div class="diff-split-cell diff-empty"></div>';
          const right = row.right
            ? `<div class="diff-split-cell diff-cell-add"><span class="diff-ln">${row.right.newLine ?? ''}</span>${renderCode(row.right.text, language)}</div>`
            : '<div class="diff-split-cell diff-empty"></div>';
          return `<div class="diff-split-row">${left}${right}</div>`;
        }).join('');
        return `<div class="diff-hunk-block"><div class="diff-hunk-title">${escapeHtml(hunk.header)}</div>${rows}</div>`;
      }).join('');
    return `<section class="diff-file-section" data-diff-file="${escapeHtml(file.path)}">
      <button class="diff-file-head" type="button" data-diff-jump="${escapeHtml(file.path)}">
        ${escapeHtml(file.path)}
        <span class="diff-file-stat"><b>+${file.additions}</b><i>-${file.deletions}</i></span>
      </button>
      <div class="diff-file-body">${hunks}</div>
    </section>`;
  }).join('');
}

export function diffFileList(files) {
  return files.map((file) => `<button type="button" class="diff-file-chip" data-diff-jump="${escapeHtml(file.path)}" title="${escapeHtml(file.path)}">
    ${escapeHtml(file.path.split('/').pop() || file.path)}
    <span><b>+${file.additions}</b><i>-${file.deletions}</i></span>
  </button>`).join('');
}

export function diffSummary(files) {
  const additions = files.reduce((sum, file) => sum + file.additions, 0);
  const deletions = files.reduce((sum, file) => sum + file.deletions, 0);
  return { files: files.length, additions, deletions };
}
