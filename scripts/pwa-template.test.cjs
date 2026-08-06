'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const projectRoot = path.resolve(__dirname, '..');
const read = (relativePath) => fs.readFileSync(path.join(projectRoot, relativePath), 'utf8');

test('scanner template loads one external Alpine controller before Alpine', () => {
  const index = read('templates/index.html');
  const scannerTemplate = read('templates/centering_tool.html');

  assert.ok(index.indexOf('/static/js/scanner.js') < index.indexOf('/static/js/alpine.min.js'));
  assert.match(scannerTemplate, /x-data="cardScanner\(/);
  assert.doesNotMatch(scannerTemplate, /x-data="\{\s*scanning:/);
  for (const game of ['pokemon', 'magic', 'one_piece', 'lorcana', 'weiss_schwarz', 'yugioh']) {
    assert.match(scannerTemplate, new RegExp(`value="${game}"`));
  }
  assert.match(scannerTemplate, /availableLanguages/);
  assert.match(scannerTemplate, /RETRY LAST CROP/);
  assert.match(scannerTemplate, /USE A CATALOG ID/);
  assert.match(scannerTemplate, /Only\s*the area inside the guides is uploaded/);
  assert.match(scannerTemplate, /data-testid="scan-progress-overlay"/);
  assert.match(scannerTemplate, /role="dialog" aria-modal="true"/);
  assert.match(scannerTemplate, /data-testid="scan-progress-elapsed"/);
  assert.match(scannerTemplate, /data-testid="scan-progress-cancel"/);
  assert.match(scannerTemplate, /:inert="scanning"/);
  assert.match(scannerTemplate, /aria-live="polite"/);

  const styles = read('static/css/styles.css');
  assert.match(styles, /\.scan-progress-overlay\s*\{/);
  assert.match(styles, /height:\s*100dvh/);
  assert.match(styles, /safe-area-inset-bottom/);
  assert.match(styles, /prefers-reduced-motion:\s*reduce/);
});

test('PWA uses one root-scoped worker and a non-sensitive offline fallback', () => {
  const index = read('templates/index.html');
  const worker = read('static/js/sw.js');
  const offline = read('static/offline.html');

  assert.match(index, /register\('\/sw\.js', \{ scope: '\/' \}\)/);
  assert.equal(fs.existsSync(path.join(projectRoot, 'static/sw.js')), false);
  assert.match(worker, /request\.mode === 'navigate'/);
  assert.match(worker, /fetch\(request\)\.catch/);
  assert.match(worker, /OFFLINE_URL/);
  assert.match(worker, /url\.pathname\.startsWith\('\/static\/'\)/);
  assert.doesNotMatch(worker, /cache\.put\(request/);
  assert.match(offline, /scanning and your private vault need a network connection/i);
});

test('manifest shortcuts reference real assets and no fabricated screenshots', () => {
  const manifest = JSON.parse(read('static/manifest.json'));
  assert.equal(manifest.scope, '/');
  assert.deepEqual(manifest.shortcuts.map(({ url }) => url), ['/?view=scan', '/?view=wantlist']);
  assert.equal(Object.hasOwn(manifest, 'screenshots'), false);

  const assetPaths = [
    ...manifest.icons.map(({ src }) => src),
    ...manifest.shortcuts.flatMap(({ icons }) => icons.map(({ src }) => src)),
  ];
  for (const assetPath of assetPaths) {
    assert.equal(fs.existsSync(path.join(projectRoot, assetPath.replace(/^\//, ''))), true, assetPath);
  }
});
