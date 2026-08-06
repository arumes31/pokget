import assert from 'node:assert/strict';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { buildStatic } from './build-static.mjs';

test('buildStatic minifies authored JavaScript and preserves other assets', async (t) => {
  const workspace = await mkdtemp(path.join(os.tmpdir(), 'pokget-static-'));
  const sourceDir = path.join(workspace, 'source');
  const outputDir = path.join(workspace, 'output');
  const sourceJavaScript = [
    'function greeting(name) {',
    "  const message = 'Hello, ' + name + '!';",
    '  return message;',
    '}',
    'window.greeting = greeting;',
    '',
  ].join('\n');
  const vendoredJavaScript = '(()=>{console.log("vendor")})();\n';
  const stylesheet = 'body { color: rebeccapurple; }\n';

  t.after(() => rm(workspace, { recursive: true, force: true }));

  await mkdir(path.join(sourceDir, 'js'), { recursive: true });
  await writeFile(path.join(sourceDir, 'js', 'app.js'), sourceJavaScript);
  await writeFile(path.join(sourceDir, 'js', 'vendor.min.js'), vendoredJavaScript);
  await writeFile(path.join(sourceDir, 'app.css'), stylesheet);

  const results = await buildStatic({ sourceDir, outputDir });
  const minifiedJavaScript = await readFile(path.join(outputDir, 'js', 'app.js'), 'utf8');

  assert.ok(Buffer.byteLength(minifiedJavaScript) < Buffer.byteLength(sourceJavaScript));
  assert.doesNotThrow(() => new Function(minifiedJavaScript));
  assert.equal(await readFile(path.join(sourceDir, 'js', 'app.js'), 'utf8'), sourceJavaScript);
  assert.equal(await readFile(path.join(outputDir, 'js', 'vendor.min.js'), 'utf8'), vendoredJavaScript);
  assert.equal(await readFile(path.join(outputDir, 'app.css'), 'utf8'), stylesheet);
  assert.deepEqual(results.map(({ path: file, status }) => [file, status]), [
    ['js/app.js', 'minified'],
    ['js/vendor.min.js', 'preserved'],
  ]);
});

test('buildStatic refuses overlapping source and output directories', async () => {
  await assert.rejects(
    buildStatic({ sourceDir: 'static', outputDir: 'static' }),
    /must be separate/,
  );
});
