const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const test = require('node:test');

const flush = () => new Promise(setImmediate);
function setup({ random = 0, reducedMotion = false } = {}) {
  function target() {
    const listeners = {};
    return {
      listeners,
      addEventListener(name, fn) { (listeners[name] ??= []).push(fn); },
      emit(name, event = {}) { for (const fn of listeners[name] ?? []) fn(event); },
    };
  }
  const styles = {};
  const root = Object.assign(target(), {
    dataset: {}, contains: () => false,
    style: { setProperty: (key, value) => { styles[key] = value; } },
  });
  const names = [0, 1, 2].map(index => ({ dataset: { cardName: String(index) }, textContent: '' }));
  const classes = new Set();
  const scene = Object.assign(target(), {
    isConnected: true, closest: () => root, querySelectorAll: () => names,
    classList: { add: value => classes.add(value), remove: value => classes.delete(value) },
    style: root.style,
  });
  const document = Object.assign(target(), { hidden: false, readyState: 'complete', querySelectorAll: () => [scene] });
  const reduced = Object.assign(target(), { matches: reducedMotion });
  const timers = new Map();
  let sequence = 0;
  const context = {
    document, AbortController, Math: Object.assign(Object.create(Math), { random: () => random }),
    matchMedia: query => query.includes('reduced-motion') ? reduced : { matches: true },
    Image: class { set src(value) { this.onload(); } },
    IntersectionObserver: class { observe() {} disconnect() {} },
    setTimeout: (fn, delay) => { timers.set(++sequence, { fn, delay }); return sequence; },
    clearTimeout: id => timers.delete(id),
    cancelAnimationFrame() {}, requestAnimationFrame() {},
  };
  vm.runInNewContext(fs.readFileSync('static/js/auth-art.js', 'utf8'), context);
  function tick(delay) {
    for (const [id, timer] of [...timers]) {
      if (timer.delay === delay) { timers.delete(id); timer.fn(); }
    }
  }
  return { root, scene, document, names, reduced, timers, tick, classes, styles };
}

test('arrival can select each complete artwork collection without adding controls', async () => {
  for (const [random, id] of [[0, 'celestial'], [0.4, 'botanical'], [0.9, 'mythic']]) {
    const state = setup({ random });
    await flush();
    assert.equal(state.root.dataset.cardDesign, id);
    assert.ok(state.names.every(label => label.textContent.length > 0));
    assert.match(state.styles['--card-atlas'], new RegExp(`${id}\\.webp`));
  }
});

test('idle artwork advances only after loading and crossfading the next collection', async () => {
  const state = setup();
  await flush();
  state.tick(14000);
  await flush();
  assert.equal(state.root.dataset.cardDesign, 'celestial');
  assert.ok(state.classes.has('is-changing'));
  state.tick(850);
  assert.equal(state.root.dataset.cardDesign, 'botanical');
  assert.equal(state.classes.has('is-changing'), false);
});

test('interaction stops rotation permanently, including a crossfade already underway', async () => {
  for (const event of ['focusin', 'keydown', 'pointerdown']) {
    const state = setup();
    await flush();
    state.tick(14000);
    await flush();
    state.root.emit(event);
    assert.equal(state.timers.size, 0);
    assert.equal(state.classes.has('is-changing'), false);
    state.document.emit('visibilitychange');
    assert.equal(state.timers.size, 0);
    assert.equal(state.root.dataset.artPaused, 'true');
  }
});

test('reduced motion shows a random static collection with no rotation timer', async () => {
  const state = setup({ reducedMotion: true, random: 0.9 });
  await flush();
  assert.equal(state.root.dataset.cardDesign, 'mythic');
  assert.equal(state.timers.size, 0);
});

test('hidden and removed scenes release their timers', async () => {
  const state = setup();
  await flush();
  state.document.hidden = true;
  state.document.emit('visibilitychange');
  assert.equal(state.timers.size, 0);
  state.document.hidden = false;
  state.document.emit('visibilitychange');
  assert.equal(state.timers.size, 1);
  state.document.emit('htmx:beforeCleanupElement', { detail: { elt: { contains: () => true } } });
  assert.equal(state.timers.size, 0);
});
