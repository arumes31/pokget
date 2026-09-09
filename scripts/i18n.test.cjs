const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const test = require('node:test');

function setup({ saved = null, blocked = false } = {}) {
  const events = {};
  let observe;
  function element(text, attributes = {}) {
    const node = { nodeType: 3, nodeValue: text };
    const el = {
      nodeType: 1, childNodes: [node], attributes,
      hasAttribute: name => Object.hasOwn(attributes, name),
      getAttribute: name => attributes[name] ?? null,
      setAttribute: (name, value) => { attributes[name] = value; },
    };
    node.parentElement = el;
    return el;
  }
  const heading = element(' Settings ', { 'data-i18n': '' });
  const cardName = element('Light');
  const field = element('', { 'data-i18n-attrs': 'placeholder aria-label', placeholder: 'Card name', 'aria-label': 'Search' });
  const nodes = [heading, cardName, field];
  const select = { value: '', matches: name => name === '[data-language-select]' };
  const document = { documentElement: {},
    addEventListener: (name, fn) => { events[name] = fn; },
    querySelectorAll: selector => selector === '[data-language-select]' ? [select] : nodes.filter(el => el.hasAttribute('data-i18n') || el.hasAttribute('data-i18n-attrs')),
  };
  const window = { localStorage: {
    getItem() { if (blocked) throw Error('blocked'); return saved; },
    setItem(key, value) { if (blocked) throw Error('blocked'); saved = value; },
  }, addEventListener: (name, fn) => { events[name] = fn; } };
  class MutationObserver { constructor(callback) { observe = callback; } observe() {} }
  vm.runInNewContext(fs.readFileSync('static/js/i18n.js', 'utf8'), { document, window, MutationObserver });
  events.DOMContentLoaded();
  return { document, window, heading, field, cardName, select, nodes, events, element,
    mutate: records => observe(records), saved: () => saved,
    choose(value) { select.value = value; events.change({ target: select }); } };
}

test('English is the default, regardless of browser language', () => {
  const app = setup();
  assert.equal(app.document.documentElement.lang, 'en');
  assert.equal(app.select.value, 'en');
  assert.equal(app.heading.childNodes[0].nodeValue, ' Settings ');
});

test('German selection translates marked copy and labels, persists, and reverses to English', () => {
  const app = setup();
  app.choose('de');
  assert.equal(app.saved(), 'de');
  assert.equal(app.heading.childNodes[0].nodeValue, ' Einstellungen ');
  assert.equal(app.field.attributes.placeholder, 'Kartenname');
  assert.equal(app.field.attributes['aria-label'], 'Suchen');
  assert.equal(app.cardName.childNodes[0].nodeValue, 'Light');
  assert.equal(setup({ saved: app.saved() }).select.value, 'de');
  app.choose('en');
  assert.equal(app.heading.childNodes[0].nodeValue, ' Settings ');
  assert.equal(app.field.attributes.placeholder, 'Card name');
});

test('invalid and unavailable storage fall back to English without disabling selection', () => {
  for (const options of [{ saved: 'fr' }, { blocked: true }]) {
    const app = setup(options);
    assert.equal(app.select.value, 'en');
    app.choose('de');
    assert.equal(app.document.documentElement.lang, 'de');
  }
});

test('HTMX swaps and Alpine text updates use the current language without stale translations', () => {
  const app = setup({ saved: 'de' });
  const added = app.element('Save changes', { 'data-i18n': '' });
  app.nodes.push(added);
  app.events['htmx:afterSwap']();
  assert.equal(added.childNodes[0].nodeValue, 'Änderungen speichern');
  const text = added.childNodes[0];
  text.nodeValue = 'Saved';
  app.mutate([{ type: 'characterData', target: text }]);
  assert.equal(text.nodeValue, 'Gespeichert');
  app.mutate([{ type: 'characterData', target: text }]);
  app.choose('en');
  assert.equal(text.nodeValue, 'Saved');
});

test('another tab can change or clear the language preference', () => {
  const app = setup();
  app.events.storage({ key: 'pokget-language', newValue: 'de' });
  assert.equal(app.select.value, 'de');
  app.events.storage({ key: 'pokget-theme', newValue: 'light' });
  assert.equal(app.select.value, 'de');
  app.events.storage({ key: null, newValue: null });
  assert.equal(app.select.value, 'en');
});

test('dynamic translations preserve user names, numbers, and literal dollar signs', () => {
  const app = setup({ saved: 'de' });
  assert.equal(app.window.PokgetI18n.t("Light's vault"), 'Sammlung von Light');
  assert.equal(app.window.PokgetI18n.t('Edit $1 Pikachu'), '$1 Pikachu bearbeiten');
  assert.equal(app.window.PokgetI18n.t('Stage 3 of 5'), 'Schritt 3 von 5');
  assert.equal(app.window.PokgetI18n.t('Retry in 1:05'), 'Erneut versuchen in 1:05');
  assert.equal(app.window.PokgetI18n.t('Unknown card title'), 'Unknown card title');
  assert.equal(app.window.PokgetI18n.t('Reading the bottom edge on this device (40%).'), 'Unterer Kartenrand wird auf diesem Gerät gelesen (40 %).');
  assert.match(app.window.PokgetI18n.t('Device OCR: timeout. Uploading the crop for server detection.'), /Zeitlimit erreicht/);
  app.mutate([{ type: 'characterData', target: { parentElement: null } }]);
});
