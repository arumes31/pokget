const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const root = path.resolve(__dirname, '..');
const read = (name) => fs.readFileSync(path.join(root, name), 'utf8');

test('mobile shell uses five primary destinations and an accessible overflow menu', () => {
  const page = read('templates/index.html');
  assert.equal((page.match(/data-nav-item/g) || []).length, 5);
  assert.match(page, /data-testid="more-navigation"/);
  assert.match(page, /aria-current/);
  assert.match(page, /scanner-active/);
});

test('shared collector-ledger primitives are available to every fragment', () => {
  const css = read('static/css/input.css');
  for (const selector of [
    '.app-page', '.page-header', '.page-title', '.page-description', '.empty-state',
    '.metric-card', '.status-badge', '.field-label', '.form-control',
  ]) {
    assert.match(css, new RegExp(selector.replace('.', '\\.') + '\\s*\\{'));
  }
  assert.match(css, /prefers-reduced-motion/);
  assert.match(css, /--color-paper/);
});

test('all primary screens use the shared mobile page structure', () => {
  for (const file of ['dashboard.html', 'wantlist.html', 'binders.html', 'error_database.html', 'trade.html']) {
    const page = read(`templates/${file}`);
    assert.match(page, /app-page/, file);
  }
});

test('scanner exposes immersive tool and compact control regions', () => {
  const scanner = read('templates/centering_tool.html');
  for (const testID of ['scanner-toolbar', 'scanner-frame', 'scanner-controls', 'scanner-privacy-note']) {
    assert.match(scanner, new RegExp(`data-testid="${testID}"`));
  }
  assert.match(scanner, /L\/R/);
  assert.match(scanner, /T\/B/);
});

test('trade comparison has explicit sides, verdict state, and honest disabled help', () => {
  const trade = read('templates/trade.html');
  assert.match(trade, /You give/);
  assert.match(trade, /You receive/);
  assert.match(trade, /data-testid="trade-verdict"/);
  assert.match(trade, /Complete both sides to export/);
});
