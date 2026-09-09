/* A disposable supervisor lets cancellation stop even a cold OCR initialization.
 * Its nested Tesseract worker does the WASM work; neither worker uploads images. */
'use strict';

const assetRoot = new URL('/static/vendor/ocr/7.0.0/', self.location.origin).href;
importScripts(`${assetRoot}tesseract.min.js`);

let engine = null;
let activeLanguage = '';
let busy = false;
let currentJob = null;
let stage = 'initializing';
function progress(nextStage, fraction = 0) {
  stage = nextStage;
  if (currentJob !== null) self.postMessage({ id: currentJob, type: 'progress', stage, progress: fraction });
}
const languages = new Set(['eng', 'deu', 'fra', 'jpn', 'chi_sim', 'chi_tra', 'kor']);

async function readPrintingBands(blob) {
  if (typeof OffscreenCanvas !== 'function' || typeof createImageBitmap !== 'function') return '';
  const bitmap = await createImageBitmap(blob);
  try {
    const scale = Math.min(2, 1800 / bitmap.width);
    const bandHeight = Math.max(1, Math.round(bitmap.height * 0.25));
    const canvas = new OffscreenCanvas(Math.round(bitmap.width * scale), Math.round(bandHeight * scale));
    const context = canvas.getContext('2d');
    const text = [];
    // TCG names occur at either edge; collector numbers are often tiny. These
    // enlarged edge passes stay on-device and precede rules text in the prompt.
    for (const top of [0, bitmap.height - bandHeight]) {
      progress(top === 0 ? 'top_band' : 'bottom_band');
      context.drawImage(bitmap, 0, top, bitmap.width, bandHeight, 0, 0, canvas.width, canvas.height);
      const band = await canvas.convertToBlob({ type: 'image/png' });
      const { data } = await engine.recognize(band, { tessedit_pageseg_mode: '6' }, { text: true });
      text.push(data.text.trim());
    }
    return text.join('\n');
  } finally { bitmap.close(); }
}

self.onmessage = async ({ data }) => {
  const { id, blob, language } = data || {};
  if (busy || !languages.has(language) || !(blob instanceof Blob) || blob.size > 10 * 1024 * 1024) {
    self.postMessage({ id, text: '', confidence: 0 });
    return;
  }
  busy = true;
  currentJob = id;
  try {
    if (!engine || activeLanguage !== language) {
      await engine?.terminate();
      progress('initializing');
      engine = await Tesseract.createWorker(language, 1, {
        workerPath: `${assetRoot}worker.min.js`,
        corePath: `${assetRoot}core`,
        langPath: `${assetRoot}lang`,
        workerBlobURL: false,
        cachePath: 'pokget-ocr-7-best-int-v1',
        gzip: true,
        logger: (message) => progress(message.status === 'loading language traineddata' ? 'loading_language' : stage, message.progress),
      });
      activeLanguage = language;
      await engine.setParameters({ tessedit_pageseg_mode: '11', user_defined_dpi: '300' });
    }
    progress('full_card');
    const { data: result } = await engine.recognize(blob, { tessedit_pageseg_mode: '11' }, { text: true });
    let bands = '';
    try { bands = await readPrintingBands(blob); } catch { /* Preserve the full-card read on older devices. */ }
    self.postMessage({ id, text: `${bands}\n${result.text}`, confidence: result.confidence });
  } catch {
    self.postMessage({ id, text: '', confidence: 0, error: true });
  } finally {
    currentJob = null;
    busy = false;
  }
};
