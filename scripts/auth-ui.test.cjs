const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const test = require('node:test');

function setup() {
  const listeners = {};
  const error = { textContent: '', focus() { this.focused = true; } };
  const attributes = {};
  const form = {
    matches: (selector) => selector === 'form[data-auth-form]',
    querySelector: () => error,
    setAttribute: (key, value) => { attributes[key] = value; },
    getAttribute: (key) => attributes[key],
    closest() { return this; },
  };
  const document = { addEventListener(name, fn) { listeners[name] = fn; } };
  const context = vm.createContext({ document });
  const source = fs.readFileSync('static/js/auth.js', 'utf8');
  vm.runInContext(source, context);
  return { listeners, error, attributes, form, context, source };
}

test('pending requests are announced and repeated submissions are cancelled', () => {
  const { listeners, form, error, attributes } = setup();
  error.textContent = 'Previous error';
  const event = { detail: { elt: form }, preventDefault() { this.cancelled = true; } };
  listeners['htmx:beforeRequest'](event);
  assert.equal(attributes['aria-busy'], 'true');
  assert.equal(error.textContent, '');
  listeners['htmx:beforeRequest'](event);
  assert.equal(event.cancelled, true);
  listeners['htmx:afterRequest'](event);
  assert.equal(attributes['aria-busy'], 'false');
});

test('backend errors remain readable as text and receive focus', () => {
  const { listeners, form, error } = setup();
  listeners['htmx:responseError']({ detail: { elt: form, xhr: { status: 401, responseText: '<b>Invalid email or password</b>' } } });
  assert.equal(error.textContent, '<b>Invalid email or password</b>');
  assert.equal(error.focused, true);
});

test('network and timeout failures restore a usable form with actionable feedback', () => {
  for (const name of ['htmx:sendError', 'htmx:timeout']) {
    const { listeners, form, error, attributes } = setup();
    listeners['htmx:beforeRequest']({ detail: { elt: form } });
    listeners[name]({ detail: { elt: form } });
    assert.equal(attributes['aria-busy'], 'false');
    assert.match(error.textContent, /try again/i);
  }
});

test('fragment reinsertion does not register duplicate document handlers', () => {
  const { listeners, context, source } = setup();
  const before = listeners['htmx:beforeRequest'];
  vm.runInContext(source, context);
  assert.equal(listeners['htmx:beforeRequest'], before);
});
