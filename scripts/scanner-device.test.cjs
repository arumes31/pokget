'use strict';
const assert = require('node:assert/strict');
const test = require('node:test');
const { createCardScanner } = require('../static/js/scanner.js');

test('progress descriptions distinguish local OCR from text matching', () => {
  const scanner = createCardScanner();
  scanner.setStatus('Reading card text on this device…', 2);
  assert.equal(scanner.scanProgressDetail, 'The cropped image is being read on this device.');
  scanner.setStatus('Matching device text with the catalog…', 3);
  assert.match(scanner.scanProgressDetail, /text only/);
});

test('scanner sends OCR text without an image, preserving scope and CSRF', async (t) => {
  const previous = { fetch: global.fetch, ocr: global.PokgetDeviceOCR };
  const calls = [];
  global.PokgetDeviceOCR = { createReader: () => ({ supports: () => true, read: async () => 'Furret 136', dispose() {} }) };
  global.fetch = async (url, options) => { calls.push({ url, options }); return Response.json({ id: 'furret', detected: 'Furret', needs_review: true }); };
  t.after(() => { global.fetch = previous.fetch; global.PokgetDeviceOCR = previous.ocr; });
  const scanner = createCardScanner({ csrfToken: 'csrf' }); scanner.notify = () => {};
  await scanner.submitPreparedBlob(new Blob(['photo']), 'card.jpg');
  assert.equal(calls.length, 1);
  assert.deepEqual(JSON.parse(calls[0].options.body), { ocr_text: 'Furret 136', lang: 'eng', game: 'pokemon' });
  assert.equal(calls[0].options.headers['X-CSRF-Token'], 'csrf');
  assert.equal(scanner.detectedID, 'furret');
  assert.equal(scanner.matchConfirmed, false);
});

test('only explicit no-match or old-server 415 falls back to one image upload', async (t) => {
  const previous = { fetch: global.fetch, ocr: global.PokgetDeviceOCR };
  global.PokgetDeviceOCR = { createReader: () => ({ supports: () => true, read: async () => 'Furret 136', dispose() {} }) };
  t.after(() => { global.fetch = previous.fetch; global.PokgetDeviceOCR = previous.ocr; });
  for (const status of [200, 415, 401, 403, 422, 429, 500]) {
    const calls = [];
    global.fetch = async (url, options) => {
      calls.push(options);
      return calls.length === 1 ? new Response(JSON.stringify({ requires_image: true }), { status }) : Response.json({});
    };
    const scanner = createCardScanner(); scanner.notify = () => {};
    await scanner.submitPreparedBlob(new Blob(['photo']), 'card.jpg');
    const fallback = [200, 415].includes(status);
    assert.equal(calls.length, fallback ? 2 : 1, `HTTP ${status}`);
    if (fallback) assert.ok(calls[1].body instanceof FormData);
  }
});

test('cancellation before device OCR finishes never uploads the photo', async (t) => {
  const previous = { fetch: global.fetch, ocr: global.PokgetDeviceOCR };
  let finish;
  global.PokgetDeviceOCR = { createReader: () => ({ supports: () => true, read: () => new Promise(resolve => { finish = resolve; }), dispose() {} }) };
  global.fetch = async () => { assert.fail('cancelled scan must not send text or images'); };
  t.after(() => { global.fetch = previous.fetch; global.PokgetDeviceOCR = previous.ocr; });
  const scanner = createCardScanner(); scanner.notify = () => {};
  const job = scanner.submitPreparedBlob(new Blob(['photo']), 'card.jpg');
  scanner.cancelScan(); finish('Furret 136'); await job;
  assert.equal(scanner.detectedID, '');
});
