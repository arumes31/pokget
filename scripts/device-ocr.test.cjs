'use strict';
const assert = require('node:assert/strict');
const test = require('node:test');
const fs = require('node:fs');
const vm = require('node:vm');
const { createReader, usableText } = require('../static/js/device-ocr.js');

function setup(options = {}) {
  const workers = [];
  class Worker {
    constructor(url) { this.url = url; this.jobs = []; this.stopped = false; workers.push(this); }
    postMessage(job) { this.jobs.push(job); }
    terminate() { this.stopped = true; }
    reply(text = 'Furret 136/197', confidence = 90, id = this.jobs.at(-1).id) { this.onmessage({ data: { id, text, confidence } }); }
  }
  return { workers, reader: createReader({ WorkerClass: Worker, ...options }) };
}

test('worker selects band and full-card segmentation on reused reads', async () => {
  const modes = [];
  let mode, creations = 0, closed = 0;
  const self = { location: { origin: 'https://pokget.test' }, postMessage() {} };
  const engine = {
    async setParameters(parameters) { mode = parameters.tessedit_pageseg_mode; },
    // Tesseract.js 7 applies recognition options temporarily, then restores its parameters.
    async recognize(blob, options) { modes.push(options.tessedit_pageseg_mode ?? mode); return { data: { text: 'Furret 136', confidence: 90 } }; },
  };
  const context = {
    self, URL, Blob, importScripts() {},
    Tesseract: { async createWorker() { creations++; return engine; } },
    async createImageBitmap() { return { width: 400, height: 600, close() { closed++; } }; },
    OffscreenCanvas: class {
      getContext() { return { drawImage() {} }; }
      async convertToBlob() { return new Blob(['band']); }
    },
  };
  vm.runInNewContext(fs.readFileSync('static/js/device-ocr-worker.js', 'utf8'), context);
  for (const id of [1, 2]) await self.onmessage({ data: { id, blob: new Blob(['card']), language: 'eng' } });
  assert.deepEqual(modes, ['11', '6', '6', '11', '6', '6']);
  assert.equal(creations, 1);
  assert.equal(closed, 2);
});

test('device OCR reuses its worker and sends only the selected language', async () => {
  const { workers, reader } = setup();
  const blob = new Blob(['photo']);
  try {
    let job = reader.read(blob, 'eng'); workers[0].reply();
    assert.equal(await job, 'Furret 136/197');
    job = reader.read(blob, 'eng'); workers[0].reply(); await job;
    assert.equal(workers.length, 1);
    assert.equal(workers[0].jobs[0].language, 'eng');
    job = reader.read(blob, 'jpn'); workers[1].reply(); await job;
    assert.equal(workers[0].stopped, true);
    assert.equal(workers.length, 2);
    assert.equal(await reader.read(blob, 'eng+jpn'), null);
  } finally { reader.dispose(); }
});

test('cancel during cold initialization terminates worker and rejects', async () => {
  const { workers, reader } = setup();
  const controller = new AbortController();
  const job = reader.read(new Blob(['photo']), 'deu', controller.signal);
  controller.abort();
  await assert.rejects(job, { name: 'AbortError' });
  assert.equal(workers[0].stopped, true);
  workers[0].reply(); // Late replies cannot resurrect a cancelled job.
  reader.dispose();
});

test('timeout, worker error and weak OCR gracefully request image fallback', async () => {
  const { workers, reader } = setup({ timeoutMS: 10 });
  assert.equal(await reader.read(new Blob(['photo']), 'eng'), null);
  assert.equal(workers[0].stopped, true);
  let job = reader.read(new Blob(['photo']), 'eng'); workers[1].onerror();
  assert.equal(await job, null);
  job = reader.read(new Blob(['photo']), 'eng'); workers[2].reply('???', 10);
  assert.equal(await job, '');
  reader.dispose();
});

test('device text is bounded and control characters are removed', () => {
  const text = usableText({ text: '噴火龍\0ex '+ '字'.repeat(8000), confidence: 90 });
  assert.ok(Buffer.byteLength(text) <= 8192);
  assert.ok(!text.includes('\0'));
  assert.equal(usableText({ text: 'Furret', confidence: NaN }), '');
});

test('progress does not finish recognition and diagnostics contain no text', async () => {
  const { workers, reader } = setup();
  const events = [];
  try {
    const job = reader.read(new Blob(['private image']), 'eng', undefined, event => events.push(event));
    workers[0].onmessage({ data: { id: 1, type: 'progress', stage: 'full_card', progress: 0.5, text: 'private text' } });
    assert.equal(workers[0].stopped, false);
    workers[0].reply('Furret 136/197');
    assert.equal(await job, 'Furret 136/197');
    assert.ok(events.some(e => e.stage === 'full_card' && e.progress === 50));
    assert.equal(events.at(-1).outcome, 'usable');
    assert.equal(JSON.stringify(events).includes('Furret'), false);
    assert.equal(JSON.stringify(events).includes('private'), false);
  } finally { reader.dispose(); }
});

test('diagnostics distinguish timeout, weak text, errors and pause', async () => {
  const { workers, reader } = setup({ timeoutMS: 10 });
  const events = [], report = e => events.push(e);
  await reader.read(new Blob(['image']), 'eng', undefined, report);
  assert.equal(events.at(-1).outcome, 'timeout');
  let job = reader.read(new Blob(['image']), 'eng', undefined, report);
  workers[1].reply('???', 5); await job;
  assert.equal(events.at(-1).outcome, 'weak_text');
  job = reader.read(new Blob(['image']), 'eng', undefined, report);
  workers[2].onerror(); await job;
  assert.equal(events.at(-1).outcome, 'error');
  job = reader.read(new Blob(['image']), 'eng', undefined, report);
  reader.dispose(); await job;
  assert.equal(events.at(-1).outcome, 'paused');
});
