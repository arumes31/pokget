'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');
const scanner = require('../static/js/scanner.js');

test('running scan survives elapsed time and remains explicitly cancellable', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout', 'setInterval'] });
  const originalFetch = global.fetch;
  let complete;
  let signal;
  global.fetch = (_url, options) => {
    signal = options.signal;
    return new Promise((resolve) => { complete = resolve; });
  };
  t.after(() => { global.fetch = originalFetch; });
  const component = scanner.createCardScanner();
  component.notify = () => {};
  const pending = component.submitPreparedBlob(new Blob(['jpeg']), 'card.jpg');
  try {
    t.mock.timers.tick(10 * 60 * 1000);
    assert.equal(signal.aborted, false, 'elapsed time must not abort a connected scan');
    assert.equal(component.scanning, true);
    component.cancelScan();
    assert.equal(signal.aborted, true, 'explicit cancel still aborts the request');
  } finally {
    complete(new Response(JSON.stringify({ detected: 'Late result', id: 'late' }), { status: 200 }));
    await pending;
    component.cancelScan();
  }
  assert.notEqual(component.detectedID, 'late');
});
