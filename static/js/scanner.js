(function registerPokgetScanner(root, factory) {
  const api = factory();

  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }

  root.PokgetScanner = api;

  if (root.document) {
    root.document.addEventListener('alpine:init', () => {
      root.Alpine.data('cardScanner', (config = {}) => api.createCardScanner(config));
    });
  }
}(typeof globalThis !== 'undefined' ? globalThis : window, () => {
  'use strict';

  const AUTO_LANGUAGE = 'eng+jpn+deu+fra+chi_sim+chi_tra+kor';
  const DEFAULT_LINES = Object.freeze({ left: 10, right: 90, top: 10, bottom: 90 });
  const MAX_FILE_BYTES = 15 * 1024 * 1024;
  const MAX_SOURCE_DIMENSION = 12000;
  const MIN_SOURCE_DIMENSION = 180;
  const MAX_OUTPUT_DIMENSION = 1800;
  const REQUEST_TIMEOUT_MS = 90000;
  const ALLOWED_IMAGE_TYPES = new Set(['image/jpeg', 'image/png', 'image/webp']);

  const LANGUAGE_OPTIONS = Object.freeze({
    pokemon: Object.freeze([
      { value: 'eng', label: 'English' },
      { value: 'jpn', label: 'Japanese' },
      { value: 'deu', label: 'German' },
      { value: 'fra', label: 'French' },
      { value: 'chi_sim', label: 'Chinese (Simplified)' },
      { value: 'chi_tra', label: 'Chinese (Traditional)' },
      { value: 'kor', label: 'Korean' },
      { value: AUTO_LANGUAGE, label: 'Auto detect' },
    ]),
    magic: Object.freeze([
      { value: 'eng', label: 'English' },
      { value: 'jpn', label: 'Japanese' },
      { value: 'deu', label: 'German' },
      { value: 'fra', label: 'French' },
      { value: AUTO_LANGUAGE, label: 'Auto detect' },
    ]),
    one_piece: Object.freeze([
      { value: 'eng', label: 'English' },
      { value: 'jpn', label: 'Japanese' },
      { value: AUTO_LANGUAGE, label: 'Auto detect' },
    ]),
    lorcana: Object.freeze([
      { value: 'eng', label: 'English' },
      { value: 'deu', label: 'German' },
      { value: 'fra', label: 'French' },
      { value: AUTO_LANGUAGE, label: 'Auto detect' },
    ]),
    weiss_schwarz: Object.freeze([
      { value: 'eng', label: 'English' },
      { value: 'jpn', label: 'Japanese' },
      { value: AUTO_LANGUAGE, label: 'Auto detect' },
    ]),
    yugioh: Object.freeze([
      { value: 'eng', label: 'English' },
      { value: 'jpn', label: 'Japanese' },
      { value: 'deu', label: 'German' },
      { value: 'fra', label: 'French' },
      { value: 'kor', label: 'Korean' },
      { value: AUTO_LANGUAGE, label: 'Auto detect' },
    ]),
  });

  const STORAGE_KEYS = Object.freeze({
    game: 'pokget_scan_game',
    language: 'pokget_scan_lang',
    camera: 'pokget_scan_camera',
  });

  function cloneDefaultLines() {
    return { ...DEFAULT_LINES };
  }

  function clamp(value, minimum, maximum) {
    return Math.max(minimum, Math.min(value, maximum));
  }

  function languagesForGame(game) {
    return LANGUAGE_OPTIONS[game] || LANGUAGE_OPTIONS.pokemon;
  }

  function normalizeLanguage(game, language) {
    const options = languagesForGame(game);
    return options.some((option) => option.value === language) ? language : options[0].value;
  }

  function sanitizeLines(value) {
    if (!value || typeof value !== 'object') return cloneDefaultLines();

    const lines = {
      left: clamp(Number(value.left), 0, 95),
      right: clamp(Number(value.right), 5, 100),
      top: clamp(Number(value.top), 0, 95),
      bottom: clamp(Number(value.bottom), 5, 100),
    };

    if (!Object.values(lines).every(Number.isFinite)
      || lines.right - lines.left < 5
      || lines.bottom - lines.top < 5) {
      return cloneDefaultLines();
    }

    return lines;
  }

  function centeringMetrics(linesValue) {
    const lines = sanitizeLines(linesValue);
    const horizontalTotal = lines.left + (100 - lines.right);
    const verticalTotal = lines.top + (100 - lines.bottom);
    const left = horizontalTotal > 0 ? Math.round((lines.left / horizontalTotal) * 100) : 50;
    const top = verticalTotal > 0 ? Math.round((lines.top / verticalTotal) * 100) : 50;

    return {
      lr: `${left}/${100 - left}`,
      tb: `${top}/${100 - top}`,
    };
  }

  function percentCropRect(width, height, linesValue) {
    const lines = sanitizeLines(linesValue);
    const x = Math.round(width * lines.left / 100);
    const y = Math.round(height * lines.top / 100);
    const right = Math.round(width * lines.right / 100);
    const bottom = Math.round(height * lines.bottom / 100);

    return {
      x: clamp(x, 0, Math.max(0, width - 1)),
      y: clamp(y, 0, Math.max(0, height - 1)),
      width: Math.max(1, right - x),
      height: Math.max(1, bottom - y),
    };
  }

  function renderedCropRect(sourceWidth, sourceHeight, containerWidth, containerHeight, linesValue, objectFit) {
    if (![sourceWidth, sourceHeight, containerWidth, containerHeight].every((value) => Number(value) > 0)) {
      throw new Error('Image dimensions are unavailable.');
    }

    const fit = objectFit === 'contain' ? 'contain' : 'cover';
    const scale = fit === 'contain'
      ? Math.min(containerWidth / sourceWidth, containerHeight / sourceHeight)
      : Math.max(containerWidth / sourceWidth, containerHeight / sourceHeight);
    const renderedWidth = sourceWidth * scale;
    const renderedHeight = sourceHeight * scale;
    const offsetX = (containerWidth - renderedWidth) / 2;
    const offsetY = (containerHeight - renderedHeight) / 2;
    const guide = percentCropRect(containerWidth, containerHeight, linesValue);
    const x = clamp((guide.x - offsetX) / scale, 0, sourceWidth - 1);
    const y = clamp((guide.y - offsetY) / scale, 0, sourceHeight - 1);
    const right = clamp((guide.x + guide.width - offsetX) / scale, x + 1, sourceWidth);
    const bottom = clamp((guide.y + guide.height - offsetY) / scale, y + 1, sourceHeight);

    return {
      x: Math.round(x),
      y: Math.round(y),
      width: Math.max(1, Math.round(right - x)),
      height: Math.max(1, Math.round(bottom - y)),
    };
  }

  function validateImageMetadata({ type, name, size, width, height }) {
    const normalizedType = String(type || '').toLowerCase();
    const extension = String(name || '').toLowerCase().match(/\.([a-z0-9]+)$/)?.[1] || '';
    const inferredType = ({ jpg: 'image/jpeg', jpeg: 'image/jpeg', png: 'image/png', webp: 'image/webp' })[extension];
    const effectiveType = normalizedType || inferredType;
    if (!ALLOWED_IMAGE_TYPES.has(effectiveType)) {
      return 'Choose a JPEG, PNG, or WebP card image.';
    }
    if (!Number.isFinite(size) || size <= 0) return 'The selected image is empty.';
    if (size > MAX_FILE_BYTES) return 'The selected image is larger than 15 MB.';
    if (width !== undefined && height !== undefined) {
      if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
        return 'The selected image could not be decoded.';
      }
      if (Math.min(width, height) < MIN_SOURCE_DIMENSION) {
        return 'The selected image is too small. Use an image at least 180 pixels on each side.';
      }
      if (Math.max(width, height) > MAX_SOURCE_DIMENSION) {
        return 'The selected image dimensions are too large.';
      }
    }
    return '';
  }

  function friendlyHTTPError(status, body) {
    const safeBody = String(body || '').replace(/\s+/g, ' ').trim().slice(0, 180);
    const known = {
      400: 'The image or scan options were not accepted.',
      401: 'Your session expired. Sign in again before scanning.',
      403: 'The scan was blocked. Refresh the page and try again.',
      404: 'The scanner endpoint is unavailable.',
      413: 'The prepared image is still too large.',
      415: 'That image format is not supported.',
      422: 'The card could not be read from this image.',
      429: 'Too many scans were submitted. Wait a moment and retry.',
    };
    if ([400, 408, 409, 422].includes(status) && safeBody && !safeBody.startsWith('<')) {
      return safeBody;
    }
    if (known[status]) return known[status];
    if (status >= 500) return 'The scanner is temporarily unavailable. Your image remains available for retry.';
    return safeBody || `The scan failed with status ${status}.`;
  }

  function safeImageURL(value, baseURL) {
    if (!value) return '';
    try {
      const base = baseURL || (typeof location !== 'undefined' ? location.href : 'https://pokget.invalid/');
      const parsed = new URL(String(value), base);
      if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') return '';
      return parsed.href;
    } catch {
      return '';
    }
  }

  function storageGet(key) {
    try {
      return localStorage.getItem(key) || '';
    } catch {
      return '';
    }
  }

  function storageSet(key, value) {
    try {
      localStorage.setItem(key, value);
    } catch {
      // Storage can be unavailable in private browsing or hardened contexts.
    }
  }

  function stopStream(stream) {
    if (!stream || typeof stream.getTracks !== 'function') return;
    stream.getTracks().forEach((track) => track.stop());
  }

  function canvasBlob(canvas, type = 'image/jpeg', quality = 0.9) {
    return new Promise((resolve, reject) => {
      canvas.toBlob((blob) => {
        if (blob) resolve(blob);
        else reject(new Error('The browser could not prepare the image.'));
      }, type, quality);
    });
  }

  async function renderCrop(source, crop, rotation = 0) {
    const scale = Math.min(1, MAX_OUTPUT_DIMENSION / Math.max(crop.width, crop.height));
    const outputWidth = Math.max(1, Math.round(crop.width * scale));
    const outputHeight = Math.max(1, Math.round(crop.height * scale));
    const normalizedRotation = ((Number(rotation) % 360) + 360) % 360;
    const swapDimensions = normalizedRotation === 90 || normalizedRotation === 270;
    const canvas = document.createElement('canvas');
    canvas.width = swapDimensions ? outputHeight : outputWidth;
    canvas.height = swapDimensions ? outputWidth : outputHeight;
    const context = canvas.getContext('2d', { alpha: false });
    if (!context) throw new Error('Canvas rendering is unavailable.');

    context.fillStyle = '#111111';
    context.fillRect(0, 0, canvas.width, canvas.height);
    context.translate(canvas.width / 2, canvas.height / 2);
    context.rotate(normalizedRotation * Math.PI / 180);
    context.drawImage(
      source,
      crop.x,
      crop.y,
      crop.width,
      crop.height,
      -outputWidth / 2,
      -outputHeight / 2,
      outputWidth,
      outputHeight,
    );

    return canvasBlob(canvas);
  }

  async function decodeImage(file) {
    if (typeof createImageBitmap === 'function') {
      const bitmap = await createImageBitmap(file);
      return {
        source: bitmap,
        width: bitmap.width,
        height: bitmap.height,
        close: () => bitmap.close(),
      };
    }

    const objectURL = URL.createObjectURL(file);
    try {
      const image = new Image();
      image.decoding = 'async';
      image.src = objectURL;
      await image.decode();
      return {
        source: image,
        width: image.naturalWidth,
        height: image.naturalHeight,
        close: () => {},
      };
    } finally {
      URL.revokeObjectURL(objectURL);
    }
  }

  function createCardScanner(config = {}) {
    return {
      scanning: false,
      scanStatus: '',
      scanStep: 0,
      scanError: '',
      lines: cloneDefaultLines(),
      metrics: centeringMetrics(DEFAULT_LINES),
      dragging: null,
      result: '',
      detectedCard: '',
      detectedID: '',
      detectedPrice: '',
      detectedImage: '',
      confidence: 0,
      needsReview: false,
      matchConfirmed: false,
      topMatches: [],
      adding: false,
      added: false,
      lang: 'eng',
      game: 'pokemon',
      cameraError: '',
      cameras: [],
      currentCamera: '',
      activeStream: null,
      abortController: null,
      operationID: 0,
      requestID: 0,
      pendingFile: null,
      previewBlob: null,
      previewURL: '',
      previewRotation: 0,
      lastScanBlob: null,
      lastScanName: '',
      manualCardID: '',
      manualCardName: '',
      csrfToken: String(config.csrfToken || ''),
      deviceChangeHandler: null,

      get availableLanguages() {
        return languagesForGame(this.game);
      },

      init() {
        const storedGame = storageGet(STORAGE_KEYS.game);
        this.game = LANGUAGE_OPTIONS[storedGame]
          ? storedGame
          : 'pokemon';
        this.lang = normalizeLanguage(this.game, storageGet(STORAGE_KEYS.language) || 'eng');
        this.currentCamera = storageGet(STORAGE_KEYS.camera);

        this.$watch('game', (game) => {
          this.lang = normalizeLanguage(game, this.lang);
          storageSet(STORAGE_KEYS.game, game);
          storageSet(STORAGE_KEYS.language, this.lang);
        });
        this.$watch('lang', (language) => storageSet(STORAGE_KEYS.language, language));
        this.setupScanner();
      },

      destroy() {
        this.cancelScan('destroyed');
        stopStream(this.activeStream);
        this.activeStream = null;
        if (this.previewURL) URL.revokeObjectURL(this.previewURL);
        if (this.deviceChangeHandler
          && typeof navigator.mediaDevices?.removeEventListener === 'function') {
          navigator.mediaDevices.removeEventListener('devicechange', this.deviceChangeHandler);
        }
      },

      notify(message, type = 'info') {
        window.dispatchEvent(new CustomEvent('notify', { detail: { msg: message, type } }));
      },

      setStatus(message, step) {
        this.scanStatus = message;
        this.scanStep = step;
      },

      resetResult() {
        this.result = '';
        this.detectedCard = '';
        this.detectedID = '';
        this.detectedPrice = '';
        this.detectedImage = '';
        this.confidence = 0;
        this.needsReview = false;
        this.matchConfirmed = false;
        this.topMatches = [];
        this.added = false;
        this.scanError = '';
      },

      resetAll() {
        this.cancelScan('reset');
        this.lines = cloneDefaultLines();
        this.updateMetrics();
        this.resetResult();
        this.clearPreview();
        this.lastScanBlob = null;
        this.lastScanName = '';
        this.manualCardID = '';
        this.manualCardName = '';
        this.scanStatus = '';
        this.scanStep = 0;
      },

      async setupScanner() {
        if (!navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== 'function') {
          this.cameraError = window.isSecureContext
            ? 'This browser does not provide camera access. Upload a photo instead.'
            : 'Camera access requires HTTPS or localhost. Upload a photo instead.';
          return;
        }

        if (typeof navigator.mediaDevices.addEventListener === 'function') {
          this.deviceChangeHandler = () => this.loadCameras();
          navigator.mediaDevices.addEventListener('devicechange', this.deviceChangeHandler);
        }
        await this.startCamera();
      },

      async loadCameras() {
        if (!navigator.mediaDevices || typeof navigator.mediaDevices.enumerateDevices !== 'function') return;
        try {
          const devices = await navigator.mediaDevices.enumerateDevices();
          this.cameras = devices.filter((device) => device.kind === 'videoinput');
          const activeDevice = this.activeStream?.getVideoTracks?.()[0]?.getSettings?.().deviceId;
          if (activeDevice) this.currentCamera = activeDevice;
        } catch (error) {
          console.warn('Unable to enumerate cameras', error);
        }
      },

      async startCamera() {
        if (!navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== 'function') return;
        this.cameraError = '';
        const requestedCamera = this.currentCamera;
        const preferred = requestedCamera
          ? { deviceId: { exact: requestedCamera }, width: { ideal: 1920 }, height: { ideal: 1080 } }
          : { facingMode: { ideal: 'environment' }, width: { ideal: 1920 }, height: { ideal: 1080 } };

        stopStream(this.activeStream);
        this.activeStream = null;

        try {
          let stream;
          try {
            stream = await navigator.mediaDevices.getUserMedia({ video: preferred, audio: false });
          } catch (error) {
            if (!requestedCamera || error.name === 'NotAllowedError') throw error;
            this.currentCamera = '';
            stream = await navigator.mediaDevices.getUserMedia({
              video: { facingMode: { ideal: 'environment' } },
              audio: false,
            });
          }

          this.activeStream = stream;
          this.$refs.video.srcObject = stream;
          await this.$refs.video.play().catch(() => {});
          const actualCamera = stream.getVideoTracks()[0]?.getSettings?.().deviceId || '';
          if (actualCamera) {
            this.currentCamera = actualCamera;
            storageSet(STORAGE_KEYS.camera, actualCamera);
          }
          await this.loadCameras();
        } catch (error) {
          console.warn('Camera unavailable', error);
          if (error.name === 'NotAllowedError' || error.name === 'SecurityError') {
            this.cameraError = 'Camera permission was denied. Allow it in browser settings or upload a photo.';
          } else if (error.name === 'NotFoundError') {
            this.cameraError = 'No camera was found. Upload a photo instead.';
          } else {
            this.cameraError = 'The camera could not start. Upload a photo or choose another camera.';
          }
        }
      },

      async switchCamera() {
        storageSet(STORAGE_KEYS.camera, this.currentCamera);
        await this.startCamera();
      },

      startGuideDrag(edge, event) {
        if (this.scanning) return;
        this.dragging = edge;
        event.currentTarget?.setPointerCapture?.(event.pointerId);
      },

      handleMove(event) {
        if (!this.dragging) return;
        const rect = this.$refs.container.getBoundingClientRect();
        if (!rect.width || !rect.height) return;
        const x = clamp(((event.clientX - rect.left) / rect.width) * 100, 0, 100);
        const y = clamp(((event.clientY - rect.top) / rect.height) * 100, 0, 100);
        if (this.dragging === 'left') this.lines.left = Math.min(x, this.lines.right - 5);
        if (this.dragging === 'right') this.lines.right = Math.max(x, this.lines.left + 5);
        if (this.dragging === 'top') this.lines.top = Math.min(y, this.lines.bottom - 5);
        if (this.dragging === 'bottom') this.lines.bottom = Math.max(y, this.lines.top + 5);
        this.updateMetrics();
      },

      finishGuideDrag() {
        this.dragging = null;
      },

      nudgeGuide(edge, delta) {
        if (this.scanning) return;
        if (edge === 'left') this.lines.left = clamp(this.lines.left + delta, 0, this.lines.right - 5);
        if (edge === 'right') this.lines.right = clamp(this.lines.right + delta, this.lines.left + 5, 100);
        if (edge === 'top') this.lines.top = clamp(this.lines.top + delta, 0, this.lines.bottom - 5);
        if (edge === 'bottom') this.lines.bottom = clamp(this.lines.bottom + delta, this.lines.top + 5, 100);
        this.updateMetrics();
      },

      updateMetrics() {
        this.lines = sanitizeLines(this.lines);
        this.metrics = centeringMetrics(this.lines);
      },

      async capture() {
        if (this.scanning) return;
        const video = this.$refs.video;
        if (!video?.videoWidth || !video?.videoHeight) {
          this.notify('Camera not ready. Wait a moment or upload a photo.', 'error');
          return;
        }

        this.resetResult();
        const operationID = ++this.operationID;
        this.scanning = true;
        this.setStatus('Preparing the guide crop…', 1);
        try {
          const rect = this.$refs.container.getBoundingClientRect();
          const objectFit = getComputedStyle(video).objectFit;
          const crop = renderedCropRect(
            video.videoWidth,
            video.videoHeight,
            rect.width,
            rect.height,
            this.lines,
            objectFit,
          );
          const blob = await renderCrop(video, crop);
          if (operationID !== this.operationID) return;
          await this.submitPreparedBlob(blob, 'camera-crop.jpg');
        } catch (error) {
          if (operationID === this.operationID) this.handleScanError(error);
        }
      },

      async handleFileUpload(event) {
        const file = event.target.files?.[0];
        event.target.value = '';
        if (!file || this.scanning) return;

        this.scanError = '';
        const basicError = validateImageMetadata(file);
        if (basicError) {
          this.scanError = basicError;
          this.notify(basicError, 'error');
          return;
        }

        let decoded;
        const operationID = ++this.operationID;
        this.scanning = true;
        this.setStatus('Preparing a private local preview…', 1);
        try {
          decoded = await decodeImage(file);
          const metadataError = validateImageMetadata({
            type: file.type,
            size: file.size,
            width: decoded.width,
            height: decoded.height,
          });
          if (metadataError) throw new Error(metadataError);

          const previewBlob = await renderCrop(decoded.source, {
            x: 0,
            y: 0,
            width: decoded.width,
            height: decoded.height,
          });
          if (operationID !== this.operationID) return;
          this.clearPreview();
          this.pendingFile = file;
          this.setPreviewBlob(previewBlob);
          this.previewRotation = 0;
          this.resetResult();
          this.notify('Photo ready. Adjust the guides, rotate if needed, then scan.', 'info');
        } catch (error) {
          if (operationID === this.operationID) {
            this.clearPreview();
            this.scanError = error.message || 'The selected image could not be opened.';
            this.notify(this.scanError, 'error');
          }
        } finally {
          decoded?.close();
          if (operationID === this.operationID) {
            this.scanning = false;
            this.scanStatus = '';
            this.scanStep = 0;
          }
        }
      },

      async rotatePreview() {
        if (!this.pendingFile || this.scanning) return;
        let decoded;
        const operationID = ++this.operationID;
        this.scanning = true;
        this.setStatus('Rotating the local preview…', 1);
        try {
          decoded = await decodeImage(this.pendingFile);
          const rotation = (this.previewRotation + 90) % 360;
          const previewBlob = await renderCrop(decoded.source, {
            x: 0,
            y: 0,
            width: decoded.width,
            height: decoded.height,
          }, rotation);
          if (operationID !== this.operationID) return;
          this.setPreviewBlob(previewBlob);
          this.previewRotation = rotation;
        } catch (error) {
          if (operationID === this.operationID) {
            this.scanError = error.message || 'The preview could not be rotated.';
            this.notify(this.scanError, 'error');
          }
        } finally {
          decoded?.close();
          if (operationID === this.operationID) {
            this.scanning = false;
            this.scanStatus = '';
            this.scanStep = 0;
          }
        }
      },

      setPreviewBlob(blob) {
        if (this.previewURL) URL.revokeObjectURL(this.previewURL);
        this.previewBlob = blob;
        this.previewURL = URL.createObjectURL(blob);
      },

      clearPreview() {
        if (this.previewURL) URL.revokeObjectURL(this.previewURL);
        this.previewBlob = null;
        this.previewURL = '';
        this.previewRotation = 0;
        this.pendingFile = null;
      },

      async scanUpload() {
        if (!this.previewBlob || this.scanning) return;
        this.resetResult();
        const operationID = ++this.operationID;
        this.scanning = true;
        this.setStatus('Preparing the guide crop…', 1);
        let decoded;
        try {
          decoded = await decodeImage(this.previewBlob);
          const rect = this.$refs.container.getBoundingClientRect();
          const crop = renderedCropRect(
            decoded.width,
            decoded.height,
            rect.width,
            rect.height,
            this.lines,
            'contain',
          );
          const blob = await renderCrop(decoded.source, crop);
          if (operationID !== this.operationID) return;
          await this.submitPreparedBlob(blob, 'upload-crop.jpg');
        } catch (error) {
          if (operationID === this.operationID) this.handleScanError(error);
        } finally {
          decoded?.close();
        }
      },

      async retryLastScan() {
        if (!this.lastScanBlob || this.scanning) return;
        this.resetResult();
        await this.submitPreparedBlob(this.lastScanBlob, this.lastScanName || 'retry.jpg', false);
      },

      async submitPreparedBlob(blob, filename, remember = true) {
        if (this.abortController) return;
        if (remember) {
          this.lastScanBlob = blob;
          this.lastScanName = filename;
        }

        this.scanning = true;
        this.scanError = '';
        const requestID = ++this.requestID;
        const controller = new AbortController();
        this.abortController = controller;
        const timeout = setTimeout(() => {
          if (this.abortController !== controller) return;
          controller.pokgetReason = 'timeout';
          controller.abort();
        }, REQUEST_TIMEOUT_MS);

        try {
          this.setStatus('Uploading the crop and running detection…', 2);
          const formData = new FormData();
          formData.append('card_image', blob, filename);
          formData.append('lang', normalizeLanguage(this.game, this.lang));
          formData.append('game', this.game);

          const response = await fetch('/api/scan', {
            method: 'POST',
            headers: { 'X-CSRF-Token': this.csrfToken },
            body: formData,
            signal: controller.signal,
          });

          this.setStatus('Reading the scanner response…', 3);
          if (!response.ok) {
            const body = await response.text();
            const error = new Error(friendlyHTTPError(response.status, body));
            error.status = response.status;
            throw error;
          }

          let data;
          try {
            data = await response.json();
          } catch {
            throw new Error('The scanner returned an unreadable response. Retry the same photo.');
          }
          this.setStatus('Validating the match…', 4);
          this.applyScanResult(data);
          this.setStatus('Scan complete', 5);

          if (this.needsReview) {
            this.notify('Review the possible printings before adding the card.', 'info');
          } else if (this.detectedID) {
            this.notify('Card detected.', 'success');
          } else {
            this.scanError = 'No confident match was found. Adjust the guides, retry, or enter a card ID.';
            this.notify(this.scanError, 'error');
          }
        } catch (error) {
          if (requestID === this.requestID) {
            this.handleScanError(error, controller.pokgetReason || '');
          }
        } finally {
          clearTimeout(timeout);
          if (requestID === this.requestID) {
            this.abortController = null;
            this.scanning = false;
          }
        }
      },

      handleScanError(error, abortReason = '') {
        if (abortReason === 'destroyed' || abortReason === 'reset') {
          this.scanError = '';
          this.scanStatus = '';
          this.scanStep = 0;
          this.scanning = false;
          return;
        }
        if (error?.name === 'AbortError') {
          this.scanError = abortReason === 'timeout'
            ? 'The scan took longer than 90 seconds. Retry the prepared image.'
            : 'Scan cancelled. The prepared image is still available.';
        } else {
          this.scanError = error?.message || 'The scan failed. Retry the prepared image.';
        }
        this.scanStatus = '';
        this.scanStep = 0;
        this.scanning = false;
        this.notify(this.scanError, abortReason === 'cancelled' ? 'info' : 'error');
      },

      cancelScan(reason = 'cancelled') {
        this.operationID += 1;
        this.requestID += 1;
        const controller = this.abortController;
        this.abortController = null;
        if (controller) {
          controller.pokgetReason = reason;
          controller.abort();
        }
        if (!this.scanning) return;
        this.scanning = false;
        this.scanStatus = '';
        this.scanStep = 0;
        if (reason === 'cancelled') {
          this.scanError = controller
            ? 'Scan cancelled. The prepared image is still available.'
            : 'Image preparation cancelled.';
          this.notify(this.scanError, 'info');
        }
      },

      applyScanResult(data) {
        const payload = data && typeof data === 'object' ? data : {};
        this.result = String(payload.text || '');
        this.detectedCard = String(payload.detected || '');
        this.detectedID = String(payload.id || '');
        this.detectedPrice = payload.price ?? '';
        this.detectedImage = safeImageURL(payload.image_url);
        this.confidence = clamp(Number(payload.confidence) || 0, 0, 100);
        this.needsReview = Boolean(payload.needs_review);
        this.matchConfirmed = !this.needsReview;
        this.topMatches = Array.isArray(payload.top_matches)
          ? payload.top_matches.slice(0, 8).map((match) => ({
            id: String(match.id || ''),
            name: String(match.name || match.id || 'Unknown printing'),
            price: match.price ?? '',
            image_url: safeImageURL(match.image_url),
            confidence: clamp(Number(match.confidence) || 0, 0, 100),
          })).filter((match) => match.id)
          : [];
        if (payload.bounds) {
          this.lines = sanitizeLines(payload.bounds);
          this.updateMetrics();
        }
      },

      selectMatch(match) {
        if (this.scanning) return;
        this.detectedCard = match.name;
        this.detectedID = match.id;
        this.detectedPrice = match.price;
        this.detectedImage = match.image_url;
        this.confidence = match.confidence;
        this.matchConfirmed = true;
        this.added = false;
      },

      useManualMatch() {
        if (this.scanning) return;
        const id = String(this.manualCardID || '').trim();
        if (!/^[a-z0-9][a-z0-9._:/-]{0,127}$/i.test(id)) {
          this.notify('Enter a valid catalog card ID.', 'error');
          return;
        }
        this.resetResult();
        this.detectedID = id;
        this.detectedCard = String(this.manualCardName || '').trim() || id;
        this.matchConfirmed = true;
        this.result = 'Manual catalog ID';
        this.notify('Manual card ID selected. Review it before adding.', 'info');
      },

      async addToCollection() {
        if (this.scanning || !this.detectedID || this.adding || this.added) return;
        if (this.needsReview && !this.matchConfirmed) {
          this.notify('Confirm the intended printing before adding it.', 'error');
          return;
        }

        this.adding = true;
        try {
          const formData = new FormData();
          formData.append('card_id', this.detectedID);
          const response = await fetch('/portfolio/add', {
            method: 'POST',
            headers: { 'X-CSRF-Token': this.csrfToken },
            body: formData,
          });
          if (!response.ok) {
            throw new Error(friendlyHTTPError(response.status, await response.text()));
          }
          this.added = true;
          this.notify('Added to collection.', 'success');
        } catch (error) {
          this.notify(error.message || 'The card could not be added.', 'error');
        } finally {
          this.adding = false;
        }
      },
    };
  }

  return Object.freeze({
    AUTO_LANGUAGE,
    DEFAULT_LINES,
    LANGUAGE_OPTIONS,
    MAX_FILE_BYTES,
    centeringMetrics,
    createCardScanner,
    friendlyHTTPError,
    languagesForGame,
    normalizeLanguage,
    percentCropRect,
    renderedCropRect,
    safeImageURL,
    sanitizeLines,
    validateImageMetadata,
  });
}));
