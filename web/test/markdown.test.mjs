import assert from 'node:assert/strict';
import test from 'node:test';
import { renderMarkdown, highlightCode, escapeHtml } from '../assets/markdown.js';
import { describeTool, parseToolInput } from '../assets/chat-format.js';

test('escapes raw HTML instead of rendering it', () => {
  const html = renderMarkdown('<img src=x onerror=alert(1)>');
  assert.ok(html.includes('&lt;img'));
  assert.ok(!html.includes('<img'));
});

test('renders headings, emphasis, inline code and safe links', () => {
  const html = renderMarkdown('# Title\n\n**bold** and *italic* with `code` and [docs](https://example.com).');
  assert.ok(html.includes('<h1>Title</h1>'));
  assert.ok(html.includes('<strong>bold</strong>'));
  assert.ok(html.includes('<em>italic</em>'));
  assert.ok(html.includes('<code>code</code>'));
  assert.ok(html.includes('href="https://example.com"'));
});

test('does not create javascript: links', () => {
  const html = renderMarkdown('[click](javascript:alert(1))');
  assert.ok(!html.includes('href="javascript:'));
});

test('renders fenced code with language and copy control', () => {
  const html = renderMarkdown('```go\nfunc main() { return 1 }\n```');
  assert.ok(html.includes('class="md-code"'));
  assert.ok(html.includes('md-code-lang">go'));
  assert.ok(html.includes('md-copy'));
  assert.ok(html.includes('tok-key">func'));
});

test('highlights diff lines', () => {
  const html = highlightCode('+added\n-removed\n@@ hunk', 'diff');
  assert.ok(html.includes('diff-add'));
  assert.ok(html.includes('diff-del'));
  assert.ok(html.includes('diff-hunk'));
});

test('renders GFM tables', () => {
  const html = renderMarkdown('| Name | Value |\n| --- | --- |\n| a | 1 |');
  assert.ok(html.includes('<table>'));
  assert.ok(html.includes('<th>Name</th>'));
  assert.ok(html.includes('<td>1</td>'));
});

test('preserves code content escaping', () => {
  const html = renderMarkdown('```html\n<script>alert(1)</script>\n```');
  assert.ok(html.includes('&lt;script&gt;'));
  assert.ok(!html.includes('<script>'));
});

test('escapes attribute-breaking quotes', () => {
  assert.equal(escapeHtml('" onmouseover="x'), '&quot; onmouseover=&quot;x');
});

test('renders footnotes with back references', () => {
  const html = renderMarkdown('A claim.[^1]\n\n[^1]: The source with **detail**.');
  assert.ok(html.includes('footnote-ref'));
  assert.ok(html.includes('href="#fn-1"'));
  assert.ok(html.includes('<section class="footnotes">'));
  assert.ok(html.includes('<li id="fn-1">'));
  assert.ok(html.includes('<strong>detail</strong>'));
});

test('footnote content is escaped', () => {
  const html = renderMarkdown('x[^a]\n\n[^a]: <img src=x onerror=alert(1)>');
  assert.ok(html.includes('&lt;img'));
  assert.ok(!html.includes('<img'));
});

test('renders nested task lists', () => {
  const html = renderMarkdown('- [ ] parent\n  - [x] child');
  assert.ok(html.includes('class="task"'));
  assert.ok(html.includes('child'));
  assert.ok(html.includes('<ul><li class="task">'));
  assert.ok(html.includes('task-check">x'));
});

test('parses JSON tool input and summarises a shell command', () => {
  const described = describeTool('bash', '{"command":"npm test","description":"Run tests"}');
  assert.equal(described.kind, 'terminal');
  assert.equal(described.label, 'Ran command');
  assert.equal(described.detail, 'npm test');
});

test('summarises a Codex file change as a diff', () => {
  const input = JSON.stringify({ type: 'fileChange', changes: [{ path: 'src/app.ts', diff: '@@ -1 +1 @@\n-old\n+new' }] });
  const described = describeTool('fileChange', input);
  assert.equal(described.kind, 'edit');
  assert.equal(described.detail, 'src/app.ts');
  assert.match(described.diff, /diff --git a\/src\/app.ts/);
});

test('invalid tool input stays raw instead of throwing', () => {
  assert.equal(parseToolInput('{broken'), null);
  const described = describeTool('custom', 'not json');
  assert.equal(described.detail, 'not json');
});
