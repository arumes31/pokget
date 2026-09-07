const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const test = require('node:test');

function setup({ saved = null, dark = false, blocked = false } = {}) {
  const events = {};
  const root = { dataset: {}, style: {}, classList: { toggle() {} } };
  const select = { value: '', matches: s => s === '[data-theme-select]' };
  const media = { matches: dark, addEventListener: (_, fn) => { events.system = fn; } };
  const storage = {
    getItem() { if (blocked) throw Error('blocked'); return saved; },
    setItem(key, value) { if (blocked) throw Error('blocked'); saved = value; },
  };
  const document = {
    documentElement: root,
    querySelectorAll: () => [select],
    addEventListener: (name, fn) => { events[name] = fn; },
  };
  const window = { matchMedia: () => media, localStorage: storage, addEventListener: (name, fn) => { events[name] = fn; } };
  vm.runInNewContext(fs.readFileSync('static/js/theme.js', 'utf8'), { document, window });
  events.DOMContentLoaded();
  return { root, select, events, media, saved: () => saved,
    choose(value) { select.value = value; events.change({ target: select }); } };
}

test('first visit follows the system and responds to live changes', () => {
  const app = setup({ dark: true });
  assert.equal(app.select.value, 'system');
  assert.equal(app.root.dataset.theme, 'dark');
  app.media.matches = false;
  app.events.system();
  assert.equal(app.root.dataset.theme, 'light');
});

test('explicit choices survive reload and ignore system changes', () => {
  for (const choice of ['light', 'dark', 'system']) {
    const app = setup();
    app.choose(choice);
    assert.equal(app.saved(), choice);
    const reload = setup({ saved: app.saved(), dark: true });
    assert.equal(reload.select.value, choice);
    assert.equal(reload.root.dataset.theme, choice === 'light' ? 'light' : 'dark');
    reload.media.matches = false;
    reload.events.system();
    assert.equal(reload.root.dataset.theme, choice === 'dark' ? 'dark' : 'light');
  }
});

test('invalid or inaccessible storage falls back to system and remains usable', () => {
  for (const options of [{ saved: 'invalid' }, { blocked: true }]) {
    const app = setup(options);
    assert.equal(app.select.value, 'system');
    app.choose('dark');
    assert.equal(app.root.dataset.theme, 'dark');
  }
});

test('HTMX replacements and other tabs keep the selector in sync', () => {
  const app = setup({ saved: 'dark' });
  app.select.value = 'system';
  app.events['htmx:afterSwap']();
  assert.equal(app.select.value, 'dark');
  app.events.storage({ key: 'pokget-theme', newValue: 'light' });
  assert.equal(app.root.dataset.theme, 'light');
  assert.equal(app.select.value, 'light');
  app.events.storage({ key: null, newValue: null });
  assert.equal(app.select.value, 'system');
});
