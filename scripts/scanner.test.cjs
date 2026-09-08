'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');

const scanner = require('../static/js/scanner.js');

test('local preview replacement preserves current image and ignores cancelled or stale reads', async (t) => {
  const originalReader = global.FileReader;
  const reads = [];
  global.FileReader = class {
    readAsDataURL(blob) { this.blob = blob; reads.push(this); }
  };
  t.after(() => { if (originalReader === undefined) delete global.FileReader; else global.FileReader = originalReader; });
  const complete = (index, value) => { reads[index].result = value; reads[index].onload(); };
  const component = scanner.createCardScanner();
  const current = new Blob(['current'], { type: 'image/jpeg' });
  component.previewBlob = current;
  component.previewURL = 'data:image/jpeg;base64,current';

  const cancelled = component.setPreviewBlob(new Blob(['cancelled']));
  assert.equal(component.previewBlob, current, 'keep the current photo while its replacement is being read');
  component.cancelScan();
  complete(0, 'data:image/jpeg;base64,cancelled');
  assert.equal(await cancelled, false);
  assert.equal(component.previewBlob, current);

  const stale = component.setPreviewBlob(new Blob(['stale']));
  const newest = new Blob(['newest'], { type: 'image/jpeg' });
  const replacement = component.setPreviewBlob(newest);
  complete(2, 'data:image/jpeg;base64,newest');
  assert.equal(await replacement, true);
  complete(1, 'data:image/jpeg;base64,stale');
  assert.equal(await stale, false);
  assert.equal(component.previewBlob, newest);
  assert.equal(component.previewURL, 'data:image/jpeg;base64,newest');

  const cleared = component.setPreviewBlob(new Blob(['cleared']));
  component.clearPreview();
  complete(3, 'data:image/jpeg;base64,cleared');
  assert.equal(await cleared, false);
  assert.equal(component.previewBlob, null);
  assert.equal(component.previewURL, '');
});

test('scanner exposes game-specific languages and repairs stale selections', () => {
  assert.deepEqual(
    scanner.languagesForGame('one_piece').map(({ value }) => value),
    ['eng', 'jpn', scanner.AUTO_LANGUAGE],
  );
  assert.ok(scanner.languagesForGame('yugioh').some(({ value }) => value === 'kor'));
  assert.equal(scanner.normalizeLanguage('lorcana', 'jpn'), 'eng');
  assert.equal(scanner.normalizeLanguage('magic', 'deu'), 'deu');
  assert.equal(scanner.languagesForGame('unsupported'), scanner.LANGUAGE_OPTIONS.pokemon);
});

test('guide geometry stays finite at perfect and malformed boundaries', () => {
  assert.deepEqual(scanner.centeringMetrics({ left: 0, right: 100, top: 0, bottom: 100 }), {
    lr: '50/50',
    tb: '50/50',
  });
  assert.deepEqual(scanner.sanitizeLines({ left: 90, right: 20, top: 10, bottom: 90 }), {
    left: 10,
    right: 90,
    top: 10,
    bottom: 90,
  });
  assert.deepEqual(scanner.percentCropRect(1000, 800, {
    left: 10,
    right: 90,
    top: 10,
    bottom: 90,
  }), { x: 100, y: 80, width: 800, height: 640 });
});

test('visible cover and contain guides map back to source pixels', () => {
  const lines = { left: 10, right: 90, top: 10, bottom: 90 };
  assert.deepEqual(scanner.renderedCropRect(1600, 900, 300, 400, lines, 'cover'), {
    x: 530,
    y: 90,
    width: 540,
    height: 720,
  });
  assert.deepEqual(scanner.renderedCropRect(1600, 900, 300, 400, lines, 'contain'), {
    x: 160,
    y: 0,
    width: 1280,
    height: 900,
  });
});

test('client image validation accepts supported metadata and rejects unsafe inputs', () => {
  assert.equal(scanner.validateImageMetadata({
    type: 'image/jpeg', size: 1024, width: 1200, height: 1600,
  }), '');
  assert.equal(scanner.validateImageMetadata({
    type: '', name: 'card.WEBP', size: 1024, width: 1200, height: 1600,
  }), '');
  assert.match(scanner.validateImageMetadata({
    type: 'image/gif', name: 'card.gif', size: 1024,
  }), /JPEG, PNG, or WebP/);
  assert.match(scanner.validateImageMetadata({
    type: 'image/png', size: 1024, width: 120, height: 900,
  }), /too small/);
});

test('API-facing text and URLs are normalized safely', () => {
  assert.equal(scanner.friendlyHTTPError(429, ''), 'Too many scans were submitted. Wait a moment and retry.');
  assert.equal(scanner.friendlyHTTPError(401, 'internal auth detail'), 'Your session expired. Sign in again before scanning.');
  assert.equal(scanner.friendlyHTTPError(422, 'No cards are available for the selected TCG'), 'No cards are available for the selected TCG');
  assert.equal(scanner.friendlyHTTPError(503, 'database stack'), 'The scanner is temporarily unavailable. Your image remains available for retry.');
  assert.equal(scanner.safeImageURL('javascript:alert(1)'), '');
  assert.equal(scanner.safeImageURL('/cards/1.png', 'https://pokget.test/vault'), 'https://pokget.test/cards/1.png');
});

test('result selection and full reset clear all transient state', () => {
  const component = scanner.createCardScanner({ csrfToken: 'token' });
  component.applyScanResult({
    detected: 'Card',
    id: 'set-1',
    confidence: 250,
    needs_review: true,
    image_url: 'data:text/html,bad',
    top_matches: [{ id: 'set-2', name: 'Printing', confidence: 82 }],
  });
  assert.equal(component.confidence, 100);
  assert.equal(component.detectedImage, '');
  assert.equal(component.topMatches.length, 1);

  component.lastScanBlob = {};
  component.manualCardID = 'set-3';
  component.lines = { left: 0, right: 100, top: 0, bottom: 100 };
  component.resetAll();
  assert.equal(component.detectedID, '');
  assert.equal(component.lastScanBlob, null);
  assert.equal(component.manualCardID, '');
  assert.deepEqual(component.metrics, { lr: '50/50', tb: '50/50' });
});

test('cancel invalidates active work and leaves the prepared crop retryable', () => {
  const component = scanner.createCardScanner();
  let aborted = false;
  let notification = '';
  component.notify = (message) => { notification = message; };
  component.scanning = true;
  component.lastScanBlob = {};
  component.abortController = { abort: () => { aborted = true; } };

  component.cancelScan();

  assert.equal(aborted, true);
  assert.equal(component.scanning, false);
  assert.equal(component.abortController, null);
  assert.equal(component.lastScanBlob !== null, true);
  assert.match(notification, /prepared image is still available/i);
});

test('full-screen progress exposes stages, elapsed time, and long-running reassurance', () => {
  const component = scanner.createCardScanner();

  component.setScanning(true);
  component.setStatus('Uploading the crop and running detection…', 2);
  component.scanElapsedSeconds = 16;

  assert.equal(component.scanning, true);
  assert.equal(component.scanProgressPercent, 45);
  assert.equal(component.scanElapsedLabel, '0:16');
  assert.match(component.scanProgressDetail, /Still processing/);

  component.setStatus('Validating the match…', 4);
  assert.equal(component.scanProgressPercent, 90);
  assert.match(component.scanProgressDetail, /Validating/);
  component.setScanning(false);
  assert.equal(component.scanTimer, null);
});

test('one submission pipeline sends the selected TCG and language', async () => {
  const originalFetch = global.fetch;
  let request;
  global.fetch = async (url, options) => {
    request = { url, options };
    return new Response(JSON.stringify({
      detected: 'Nami',
      id: 'op01-016',
      confidence: 91,
      image_url: 'https://cards.example/nami.jpg',
    }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  };

  try {
    const component = scanner.createCardScanner({ csrfToken: 'csrf' });
    component.game = 'one_piece';
    component.lang = 'jpn';
    component.notify = () => {};
    const crop = new Blob(['jpeg'], { type: 'image/jpeg' });

    await component.submitPreparedBlob(crop, 'crop.jpg');

    assert.equal(request.url, '/api/scan');
    assert.equal(request.options.headers['X-CSRF-Token'], 'csrf');
    assert.equal(request.options.body.get('game'), 'one_piece');
    assert.equal(request.options.body.get('lang'), 'jpn');
    assert.equal(request.options.body.get('card_image').name, 'crop.jpg');
    assert.equal(component.detectedID, 'op01-016');
    assert.equal(component.lastScanBlob, crop);
    assert.equal(component.scanning, false);
  } finally {
    global.fetch = originalFetch;
  }
});

test('a late cancelled scan response cannot replace a newer printing selection', async () => {
  const originalFetch = global.fetch;
  let complete;
  global.fetch = () => new Promise((resolve) => { complete = resolve; });
  try {
    const component = scanner.createCardScanner();
    component.notify = () => {};
    const pending = component.submitPreparedBlob(new Blob(['jpeg']), 'card.jpg');
    component.cancelScan();
    component.applyScanResult({ detected: 'New selection', id: 'new-printing', needs_review: true });
    complete(new Response(JSON.stringify({ detected: 'Old result', id: 'old-printing' }), { status: 200 }));
    await pending;
    assert.equal(component.detectedID, 'new-printing');
    assert.equal(component.matchConfirmed, false);
  } finally {
    global.fetch = originalFetch;
  }
});

test('an uncertain printing must be inspected and explicitly confirmed before saving', async () => {
  const component = scanner.createCardScanner();
  component.notify = () => {};
  component.applyScanResult({
    detected: 'Pikachu', id: 'base-58', needs_review: true,
    set: 'Base Set', collector_number: '58', language: 'en',
    top_matches: [{ id: 'jungle-60', name: 'Pikachu', set: 'Jungle', collector_number: '60', language: 'de' }],
  });
  assert.equal(component.detectedSet, 'Base Set');
  component.selectMatch(component.topMatches[0]);
  assert.equal(component.detectedSet, 'Jungle');
  assert.equal(component.detectedNumber, '60');
  assert.equal(component.detectedLanguage, 'de');
  assert.equal(component.matchConfirmed, false);
  await component.addToCollection();
  assert.equal(component.adding, false);
  component.confirmMatch();
  assert.equal(component.matchConfirmed, true);
  component.reviewMatches();
  assert.equal(component.matchConfirmed, false);
  component.resetResult();
  assert.equal(component.detectedSet, '');
  assert.equal(component.detectedNumber, '');
  assert.equal(component.detectedLanguage, '');
});

test('missing catalog preserves the selected language and crop without an automatic retry', async () => {
  const originalFetch = global.fetch;
  const requests = [];
  global.fetch = async (url, options) => {
    requests.push({ url, options });
    if (requests.length === 1) {
      return new Response('No cards are available for the selected TCG and language\n', { status: 422 });
    }
    return new Response(JSON.stringify({
      detected: 'Pikachu',
      id: 'sv1-025',
      confidence: 94,
    }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  };

  try {
    const component = scanner.createCardScanner({ csrfToken: 'csrf' });
    component.game = 'pokemon';
    component.lang = 'deu';
    component.notify = () => {};
    const crop = new Blob(['jpeg'], { type: 'image/jpeg' });

    await component.submitPreparedBlob(crop, 'crop.jpg');

    assert.equal(requests.length, 1);
    assert.equal(requests[0].options.body.get('lang'), 'deu');
    assert.equal(component.lang, 'deu');
    assert.equal(component.detectedID, '');
    assert.match(component.scanError, /catalog.*language.*sync/i);
    assert.equal(component.lastScanBlob, crop);
    assert.equal(component.scanning, false);
  } finally {
    global.fetch = originalFetch;
  }
});
