import assert from 'node:assert/strict';
import test from 'node:test';
import { parseUnifiedDiff, renderUnifiedDiff, renderSplitDiff, diffFileList, diffSummary } from '../assets/diff.js';

const SAMPLE = `diff --git a/src/app.js b/src/app.js
index 1111111..2222222 100644
--- a/src/app.js
+++ b/src/app.js
@@ -1,4 +1,5 @@
 const a = 1;
-const b = 2;
+const b = 3;
+const c = 4;
 export { a };
@@ -10,3 +11,2 @@ function run() {
 keep();
-remove();
 done();
diff --git a/README.md b/README.md
new file mode 100644
--- /dev/null
+++ b/README.md
@@ -0,0 +1,2 @@
+# AgentNexus
+Multi-agent cockpit.
`;

test('parses files, hunks and line numbers', () => {
  const files = parseUnifiedDiff(SAMPLE);
  assert.equal(files.length, 2);
  const [app, readme] = files;
  assert.equal(app.path, 'src/app.js');
  assert.equal(app.hunks.length, 2);
  assert.equal(app.additions, 2);
  assert.equal(app.deletions, 2);
  const first = app.hunks[0];
  assert.equal(first.oldStart, 1);
  assert.equal(first.newStart, 1);
  assert.deepEqual(first.lines.map((line) => line.type), ['context', 'del', 'add', 'add', 'context']);
  assert.equal(first.lines[0].oldLine, 1);
  assert.equal(first.lines[2].newLine, 2);
  assert.equal(readme.path, 'README.md');
  assert.equal(readme.additions, 2);
});

test('summarises total changes', () => {
  const summary = diffSummary(parseUnifiedDiff(SAMPLE));
  assert.deepEqual(summary, { files: 2, additions: 4, deletions: 2 });
});

test('renders unified and split layouts with escaping', () => {
  const files = parseUnifiedDiff(SAMPLE.replace('const c = 4;', 'const c = "<img onerror=x>";'));
  const unified = renderUnifiedDiff(files);
  assert.ok(unified.includes('diff-row-add'));
  assert.ok(unified.includes('diff-row-del'));
  assert.ok(unified.includes('&lt;img onerror=x&gt;'));
  assert.ok(!unified.includes('<img onerror'));
  const split = renderSplitDiff(files);
  assert.ok(split.includes('diff-cell-add'));
  assert.ok(split.includes('diff-cell-del'));
  assert.ok(split.includes('diff-split-row'));
});

test('builds file chips and reports empty input', () => {
  const files = parseUnifiedDiff(SAMPLE);
  const chips = diffFileList(files);
  assert.ok(chips.includes('app.js'));
  assert.ok(chips.includes('+2'));
  assert.ok(chips.includes('-2'));
  assert.deepEqual(parseUnifiedDiff(''), []);
  assert.deepEqual(parseUnifiedDiff('not a diff'), []);
});

test('marks binary diffs without line content', () => {
  const files = parseUnifiedDiff('diff --git a/logo.png b/logo.png\nBinary files a/logo.png and b/logo.png differ\n');
  assert.equal(files.length, 1);
  assert.equal(files[0].binary, true);
  assert.ok(renderUnifiedDiff(files).includes('Binary file differs'));
});
