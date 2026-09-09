(function registerDeviceOCR(root, factory) {
  const api = factory(root);
  if (typeof module === 'object' && module.exports) module.exports = api;
  root.PokgetDeviceOCR = api;
}(typeof globalThis !== 'undefined' ? globalThis : window, (root) => {
  'use strict';

  const LANGUAGES = new Set(['eng', 'deu', 'fra', 'jpn', 'chi_sim', 'chi_tra', 'kor']);
  const WORKER_URL = '/static/js/device-ocr-worker.js?v=2';
  const STAGES = new Set(['initializing', 'loading_language', 'full_card', 'top_band', 'bottom_band']);

  function usableText(data) {
    if (!data || Number(data.confidence) < 35 || !Number.isFinite(Number(data.confidence))) return '';
    const text = String(data.text || '').replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f]/g, ' ').trim();
    // 2,000 Unicode code points fit the server's 8 KiB UTF-8 limit.
    const bounded = Array.from(text).slice(0, 2000).join('');
    return (bounded.match(/\p{L}/gu) || []).length >= 4 ? bounded : '';
  }

  function createReader({ WorkerClass = root.Worker, timeoutMS = 20000, idleMS = 60000 } = {}) {
    let worker = null;
    let language = '';
    let pending = null;
    let idleTimer = null;
    let sequence = 0;

    function terminate() {
      clearTimeout(idleTimer);
      idleTimer = null;
      worker?.terminate();
      worker = null;
      language = '';
    }

    function dispose() {
      if (pending) pending(null, false, 'paused');
      terminate();
    }

    function supports(lang) {
      return typeof WorkerClass === 'function' && typeof root.WebAssembly === 'object' && LANGUAGES.has(lang);
    }

    function read(blob, lang, signal, onProgress) {
      if (signal?.aborted) return Promise.reject(new DOMException('Aborted', 'AbortError'));
      if (!supports(lang)) return Promise.resolve(null);
      // A scanner owns at most one job. Superseding it releases the old worker.
      if (pending) dispose();
      clearTimeout(idleTimer);
      if (language !== lang) terminate();
      const id = ++sequence;
      const started = Date.now();
      let lastProgress = '';
      const report = (event) => {
        const key = JSON.stringify(event);
        if (key === lastProgress) return;
        lastProgress = key;
        try { onProgress?.({ ...event, duration_ms: Math.max(0, Date.now() - started) }); }
        catch { /* Diagnostics must never interrupt recognition or cleanup. */ }
      };
      return new Promise((resolve, reject) => {
        let timer;
        const finish = (text, aborted = false, outcome = text ? 'usable' : 'weak_text') => {
          if (pending !== finish) return;
          pending = null;
          clearTimeout(timer);
          signal?.removeEventListener('abort', abort);
          if (!text || aborted) terminate();
          else idleTimer = setTimeout(terminate, idleMS);
          report({ outcome: aborted ? 'cancelled' : outcome });
          if (aborted) reject(new DOMException('Aborted', 'AbortError'));
          else resolve(text);
        };
        const abort = () => finish(null, true);
        pending = finish;
        signal?.addEventListener('abort', abort, { once: true });
        timer = setTimeout(() => finish(null, false, 'timeout'), timeoutMS);
        report({ stage: 'initializing', progress: 0 });
        try {
          if (!worker) {
            worker = new WorkerClass(WORKER_URL);
            language = lang;
          }
          worker.onmessage = (event) => {
            if (event.data?.id !== id || pending !== finish) return;
            if (event.data.type === 'progress') {
              if (STAGES.has(event.data.stage)) {
                const progress = Number(event.data.progress);
                report({ stage: event.data.stage, progress: Number.isFinite(progress) ? Math.round(Math.max(0, Math.min(1, progress)) * 10) * 10 : 0 });
              }
              return;
            }
            const text = usableText(event.data);
            finish(text, false, event.data.error ? 'error' : text ? 'usable' : 'weak_text');
          };
          worker.onerror = () => finish(null, false, 'error');
          worker.onmessageerror = () => finish(null, false, 'error');
          worker.postMessage({ id, blob, language: lang });
        } catch {
          finish(null, false, 'error');
        }
      });
    }

    return { supports, read, dispose };
  }

  return { createReader, usableText };
}));
