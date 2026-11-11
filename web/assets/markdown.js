/* Safe, dependency-free Markdown and code rendering for the AgentNexus chat.
 *
 * The renderer escapes all input before emitting tags. Raw HTML is never
 * interpreted, and links are restricted to http(s) and mailto URLs.
 */

const HTML_ESCAPES = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
};

export function escapeHtml(value) {
  return String(value ?? '').replace(/[&<>"']/g, (ch) => HTML_ESCAPES[ch]);
}

const LANGUAGE_ALIASES = {
  js: 'javascript', jsx: 'javascript', mjs: 'javascript', cjs: 'javascript',
  ts: 'typescript', tsx: 'typescript',
  py: 'python', sh: 'bash', shell: 'bash', zsh: 'bash', ps1: 'powershell',
  yml: 'yaml', md: 'markdown', rs: 'rust', cs: 'csharp', cpp: 'cpp', cxx: 'cpp',
  patch: 'diff',
};

const KEYWORDS = {
  javascript: 'const let var function return if else for while do switch case break continue new class extends import export from default async await try catch finally throw typeof instanceof in of this null undefined true false',
  typescript: 'const let var function return if else for while do switch case break continue new class extends implements interface type enum import export from default async await try catch finally throw typeof instanceof in of this null undefined true false readonly public private protected abstract as satisfies',
  python: 'def class return if elif else for while break continue import from as try except finally raise with lambda yield async await pass global nonlocal True False None and or not in is',
  go: 'package import func var const type struct interface map chan go defer select case default if else for range switch break continue return fallthrough goto nil true false',
  rust: 'fn let mut pub use mod struct enum trait impl for while loop if else match return async await move where crate super self Self true false unsafe dyn const static ref',
  java: 'public private protected class interface enum extends implements import package static final void int long double float boolean char byte short if else for while do switch case break continue return new try catch finally throw throws this super null true false',
  csharp: 'public private protected internal class interface enum struct record namespace using static readonly const void int long double float bool string var if else for foreach while do switch case break continue return new try catch finally throw this base null true false async await',
  bash: 'if then else elif fi for while do done case esac function in export local readonly return exit set unset echo cd pwd source alias',
  sql: 'select from where join left right inner outer on group by order having insert into values update set delete create table index view drop alter add primary key foreign references and or not null is as limit offset union all distinct',
  json: 'true false null',
  yaml: 'true false null',
  powershell: 'param function if else elseif foreach for while do switch return throw try catch finally begin process end filter in',
};

const LITERALS = new Set(['true', 'false', 'null', 'undefined', 'nil', 'None', 'True', 'False']);

function normalizeLang(lang) {
  const value = String(lang || '').trim().toLowerCase();
  return LANGUAGE_ALIASES[value] || value;
}

function commentPattern(lang) {
  if (['python', 'bash', 'yaml', 'toml', 'ruby'].includes(lang)) return '#[^\\n]*';
  if (lang === 'sql') return '--[^\\n]*';
  if (lang === 'html' || lang === 'xml') return '<!--[\\s\\S]*?-->';
  if (['javascript', 'typescript', 'go', 'rust', 'java', 'csharp', 'cpp', 'c', 'php', 'css'].includes(lang)) {
    return '\\/\\*[\\s\\S]*?\\*\\/|\\/\\/[^\\n]*';
  }
  return null;
}

function wrapToken(text, cls) {
  return `<span class="tok-${cls}">${escapeHtml(text)}</span>`;
}

/** Highlight one code block. Unknown languages are escaped but not coloured. */
export function highlightCode(code, lang) {
  const language = normalizeLang(lang);
  if (language === 'diff' || language === 'patch') return highlightDiff(code);
  const keywords = new Set((KEYWORDS[language] || '').split(/\s+/).filter(Boolean));
  const comment = commentPattern(language);
  const parts = [];
  if (comment) parts.push(`(${comment})`);
  parts.push(`("(?:\\\\.|[^"\\\\])*"|'(?:\\\\.|[^'\\\\])*'|\\x60(?:\\\\.|[^\\x60\\\\])*\\x60)`);
  parts.push('(\\b\\d+(?:\\.\\d+)?\\b)');
  parts.push('([A-Za-z_$][A-Za-z0-9_$]*)');
  const re = new RegExp(parts.join('|'), 'g');
  const source = String(code ?? '');
  let out = '';
  let last = 0;
  let match;
  while ((match = re.exec(source))) {
    out += escapeHtml(source.slice(last, match.index));
    const token = match[0];
    if (match[1] !== undefined) out += wrapToken(token, 'com');
    else if (match[2] !== undefined) out += wrapToken(token, 'str');
    else if (match[3] !== undefined) out += wrapToken(token, 'num');
    else if (keywords.has(token)) out += wrapToken(token, 'key');
    else if (LITERALS.has(token)) out += wrapToken(token, 'lit');
    else out += escapeHtml(token);
    last = match.index + token.length;
  }
  out += escapeHtml(source.slice(last));
  return out;
}

function highlightDiff(code) {
  return String(code ?? '').split('\n').map((line) => {
    let cls = '';
    if (/^(diff --git|index |--- |\+\+\+ )/.test(line)) cls = 'file';
    else if (/^@@/.test(line)) cls = 'hunk';
    else if (/^\+/.test(line)) cls = 'add';
    else if (/^-/.test(line)) cls = 'del';
    const escaped = escapeHtml(line);
    return cls ? `<span class="diff-${cls}">${escaped}</span>` : escaped;
  }).join('\n');
}

function codeBlock(code, lang) {
  const language = normalizeLang(lang);
  const label = language || 'text';
  const safeLanguage = /^[a-z0-9+#._-]+$/i.test(language) ? language : 'text';
  return `<div class="md-code"><div class="md-code-head"><span class="md-code-lang">${escapeHtml(label)}</span><button class="md-copy" type="button" aria-label="Copy code">Copy</button></div><pre><code class="language-${safeLanguage}">${highlightCode(code, language)}</code></pre></div>`;
}

function isTableSeparator(line) {
  return /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(line);
}

function splitTableRow(line) {
  let value = line.trim();
  if (value.startsWith('|')) value = value.slice(1);
  if (value.endsWith('|')) value = value.slice(0, -1);
  return value.split('|').map((cell) => cell.trim());
}

function renderTable(header, rows) {
  const head = splitTableRow(header).map((cell) => `<th>${inline(cell)}</th>`).join('');
  const body = rows.map((row) => `<tr>${splitTableRow(row).map((cell) => `<td>${inline(cell)}</td>`).join('')}</tr>`).join('');
  return `<div class="table-wrap md-table"><table><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table></div>`;
}

function renderList(lines, ordered) {
  const items = [];
  for (const line of lines) {
    const match = ordered ? line.match(/^\s*\d+\.\s+(.*)$/) : line.match(/^\s*[-*+]\s+(.*)$/);
    if (match) {
      let text = match[1];
      const task = text.match(/^\[( |x|X)\]\s+(.*)$/);
      items.push(task
        ? `<li class="task"><span class="task-check">${task[1].toLowerCase() === 'x' ? 'x' : ''}</span>${inline(task[2])}</li>`
        : `<li>${inline(text)}</li>`);
    } else if (items.length) {
      items[items.length - 1] = items[items.length - 1].replace(/<\/li>$/, `<br>${inline(line.trim())}</li>`);
    }
  }
  return ordered ? `<ol>${items.join('')}</ol>` : `<ul>${items.join('')}</ul>`;
}

function renderBlocks(source) {
  const lines = String(source ?? '').replace(/\r\n?/g, '\n').split('\n');
  const out = [];
  let paragraph = [];
  const flush = () => {
    if (!paragraph.length) return;
    out.push(`<p>${paragraph.map((line) => inline(line)).join('<br>')}</p>`);
    paragraph = [];
  };
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i];
    const fence = line.match(/^\s*```([\w+#.-]*)\s*$/);
    if (fence) {
      flush();
      const body = [];
      i += 1;
      while (i < lines.length && !/^\s*```\s*$/.test(lines[i])) {
        body.push(lines[i]);
        i += 1;
      }
      out.push(codeBlock(body.join('\n'), fence[1]));
      continue;
    }
    if (!line.trim()) { flush(); continue; }
    const heading = line.match(/^(#{1,4})\s+(.+)$/);
    if (heading) {
      flush();
      const level = Math.min(4, heading[1].length);
      out.push(`<h${level}>${inline(heading[2])}</h${level}>`);
      continue;
    }
    if (/^\s*(?:---+|\*\*\*+)\s*$/.test(line)) { flush(); out.push('<hr>'); continue; }
    if (/^\s*>\s?/.test(line)) {
      flush();
      const quote = [];
      while (i < lines.length && /^\s*>\s?/.test(lines[i])) {
        quote.push(lines[i].replace(/^\s*>\s?/, ''));
        i += 1;
      }
      i -= 1;
      out.push(`<blockquote>${renderBlocks(quote.join('\n'))}</blockquote>`);
      continue;
    }
    if (/^\s*[-*+]\s+/.test(line)) {
      flush();
      const list = [];
      while (i < lines.length && (/^\s*[-*+]\s+/.test(lines[i]) || (list.length && lines[i].trim() && !/^\s*\d+\.\s+/.test(lines[i]) && !/^\s*(```|#)/.test(lines[i])))) {
        list.push(lines[i]);
        i += 1;
      }
      i -= 1;
      out.push(renderList(list, false));
      continue;
    }
    if (/^\s*\d+\.\s+/.test(line)) {
      flush();
      const list = [];
      while (i < lines.length && (/^\s*\d+\.\s+/.test(lines[i]) || (list.length && lines[i].trim() && !/^\s*[-*+]\s+/.test(lines[i]) && !/^\s*(```|#)/.test(lines[i])))) {
        list.push(lines[i]);
        i += 1;
      }
      i -= 1;
      out.push(renderList(list, true));
      continue;
    }
    if (line.includes('|') && i + 1 < lines.length && isTableSeparator(lines[i + 1])) {
      flush();
      const header = line;
      const rows = [];
      i += 2;
      while (i < lines.length && lines[i].includes('|') && lines[i].trim()) {
        rows.push(lines[i]);
        i += 1;
      }
      i -= 1;
      out.push(renderTable(header, rows));
      continue;
    }
    paragraph.push(line);
  }
  flush();
  return out.join('\n');
}

function inline(text) {
  let source = escapeHtml(text);
  const tokens = [];
  const stash = (html) => {
    tokens.push(html);
    return `\uE000${tokens.length - 1}\uE001`;
  };
  source = source.replace(/`([^`\n]+)`/g, (_match, code) => stash(`<code>${code}</code>`));
  source = source.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+|mailto:[^\s)]+)\)/g, (_match, label, url) =>
    stash(`<a href="${url}" target="_blank" rel="noreferrer noopener">${label}</a>`));
  source = source.replace(/(^|[\s(])((?:https?:\/\/)[^\s<)]+)/g, (_match, lead, url) =>
    `${lead}${stash(`<a href="${url}" target="_blank" rel="noreferrer noopener">${url}</a>`)}`);
  source = source.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>');
  source = source.replace(/~~([^~\n]+)~~/g, '<del>$1</del>');
  source = source.replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, '$1<em>$2</em>');
  source = source.replace(/\uE000(\d+)\uE001/g, (_match, index) => tokens[Number(index)] || '');
  return source;
}

/** Render Markdown to safe HTML. */
export function renderMarkdown(source) {
  return renderBlocks(source);
}
