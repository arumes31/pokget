'use strict';
const assert = require('node:assert/strict');
const test = require('node:test');
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
