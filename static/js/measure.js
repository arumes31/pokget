(function registerMeasure(root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  root.PokgetMeasure = api;
  root.document?.addEventListener('alpine:init', () => root.Alpine.data('cardMeasure', api.createMeasure));
})(typeof globalThis !== 'undefined' ? globalThis : window, () => {
  'use strict';
  const clamp = (n, low, high) => Math.max(low, Math.min(high, n));
  const clone = value => JSON.parse(JSON.stringify(value));
  const DEFAULT_GUIDES = {
    outer: {
      left: 10,
      right: 90,
      top: 5,
      bottom: 95
    },
    inner: {
      left: 15,
      right: 85,
      top: 10,
      bottom: 90
    }
  };
  const EDGES = [ 'left', 'right', 'top', 'bottom' ];
  const SIDES = [ 'front', 'back' ];
  const MAX_BYTES = 15 * 1024 * 1024;
  const emptySide = () => ({
    image: '',
    width: 0,
    height: 0,
    guides: clone(DEFAULT_GUIDES),
    confirmed: false,
    original: ''
  });
  const row = (grade, label, front, back, extra = {}) => ({
    grade: grade,
    label: label,
    front: front,
    back: back,
    ...extra
  });
  // Explicit published thresholds only. Half grades and unpublished criteria are not interpolated.
    const STANDARDS = [ {
    company: 'PSA',
    url: 'https://www.psacard.com/gradingstandards',
    note: 'Published approximate limits; grader discretion and overall condition still apply.',
    rows: [ row('10', 'Gem Mint', 55, 75), row('9', 'Mint', 60, 90), row('8', 'NM–MT', 65, 90), row('7', 'Near Mint', 70, 90), row('6', 'EX–MT', 80, 90), row('5', 'Excellent', 85, 90) ]
  }, {
    company: 'BGS',
    url: 'https://www.beckett.com/grading/scale',
    note: 'Published BGS criteria. 9.5 requires one front axis at 50/50. Intermediate grades are not inferred.',
    rows: [ row('10', 'Pristine', 50, 60), row('9.5', 'Gem Mint', 55, 60, {
      perfectAxis: true
    }), row('9', 'Mint', 55, 70), row('8', 'NM–MT', 60, 80), row('7', 'Near Mint', 65, 90), row('6', 'EX–MT', 70, 95), row('5', 'Excellent', 75, 95), row('4', 'Very Good–Excellent', 80, 100), row('3', 'Very Good', 85, 100), row('2', 'Good', 90, 100), row('1', 'Poor', 100, 100) ]
  }, {
    company: 'CGC',
    url: 'https://www.cgccards.com/card-grading/grading-scale/',
    note: 'TCG reference: numeric lower-grade limits are not consistently published. Mint+ depends on more than centering.',
    rows: [ row('10 P', 'Pristine', 50, 50), row('10', 'Gem Mint', 55, 75) ]
  }, {
    company: 'SGC',
    url: 'https://www.gosgc.com/card-grading/scale',
    note: 'SGC lists one centering figure per grade, without a separate back threshold. Only the front is compared here.',
    rows: [ row('10 P', 'Pristine', 50, null), row('10', 'Gem Mint', 55, null), row('9', 'Mint', 60, null), row('8.5', 'NM–MT+', 65, null) ]
  }, {
    company: 'TAG',
    url: 'https://taggrading.com/pages/rubric',
    note: 'TCG thresholds. Sports cards have different back tolerances. TAG uses multiple measurements and its own scoring system; these are approximate centering limits.',
    rows: [ row('10 P', 'Pristine', 51, 52), row('10', 'Gem Mint', 55, 65), row('9', 'Mint', 60, 75), row('8.5', 'NM–MT+', 62.5, 85), row('8', 'NM–MT', 65, 95), row('7.5', 'Near Mint+', 67.5, null), row('7', 'Near Mint', 70, null) ]
  }, {
    company: 'ACE',
    url: 'https://acegrading.com/grading-scale',
    note: 'Gem Mint requires better than 60/40. Lower rows interpret the stated front/back splits as maximum imbalance; ACE’s prose uses “greater than”.',
    rows: [ row('10', 'Gem Mint', 60, 60, {
      strict: true
    }), row('9', 'Mint', 65, 70), row('8', 'NM–MT', 70, 75), row('7', 'Near Mint', 75, 80), row('6', 'EX–MT', 80, 80), row('3', 'Good', 85, 85) ]
  } ];
  function measureBorders(guides) {
    if (!guides?.outer || !guides?.inner) return null;
    const {outer: o, inner: i} = guides;
    if (![ o, i ].every(r => EDGES.every(k => Number.isFinite(r[k]) && r[k] >= 0 && r[k] <= 100))) return null;
    if (!(o.left < i.left && i.left < i.right && i.right < o.right && o.top < i.top && i.top < i.bottom && i.bottom < o.bottom)) return null;
    const lr = (i.left - o.left) / (i.left - o.left + o.right - i.right) * 100;
    const tb = (i.top - o.top) / (i.top - o.top + o.bottom - i.bottom) * 100;
    return {
      lr: lr,
      tb: tb,
      worst: Math.max(lr, 100 - lr, tb, 100 - tb)
    };
  }
  function guideLimits(guides, group, edge) {
    const horizontal = edge === 'left' || edge === 'right';
    const order = horizontal ? [ [ 'outer', 'left' ], [ 'inner', 'left' ], [ 'inner', 'right' ], [ 'outer', 'right' ] ] : [ [ 'outer', 'top' ], [ 'inner', 'top' ], [ 'inner', 'bottom' ], [ 'outer', 'bottom' ] ];
    const index = order.findIndex(([g, e]) => g === group && e === edge);
    if (index < 0) throw new Error('Unknown guide');
    return [ index ? guides[order[index - 1][0]][order[index - 1][1]] + .01 : 0, index < 3 ? guides[order[index + 1][0]][order[index + 1][1]] - .01 : 100 ];
  }
  function moveGuide(guides, group, edge, value) {
    const next = clone(guides);
    const [low, high] = guideLimits(guides, group, edge);
    if (Number.isFinite(Number(value))) next[group][edge] = clamp(Number(value), low, high);
    return next;
  }
  function fitsGrade(row, metrics, side) {
    if (!metrics || !SIDES.includes(side) || !Number.isFinite(row[side]) || ![ metrics.lr, metrics.tb ].every(v => Number.isFinite(v) && v >= 0 && v <= 100)) return false;
    const axes = [ metrics.lr, metrics.tb ].map(v => Math.max(v, 100 - v));
    const fits = row.strict ? Math.max(...axes) < row[side] : Math.max(...axes) <= row[side] + 1e-10;
    return fits && !(side === 'front' && row.perfectAxis && Math.min(...axes) > 50 + 1e-10);
  }
  function gradeFor(company, metrics, side) {
    return STANDARDS.find(s => s.company === company)?.rows.find(row => fitsGrade(row, metrics, side)) || null;
  }
  function gradeForBoth(company, front, back) {
    return STANDARDS.find(s => s.company === company)?.rows.find(row => fitsGrade(row, front, 'front') && fitsGrade(row, back, 'back')) || null;
  }
  function croppedGuides(guides) {
    if (!measureBorders(guides)) throw new Error('Align the eight guides before cropping.');
    const {outer: o, inner: i} = guides;
    return {
      outer: {
        left: 0,
        right: 100,
        top: 0,
        bottom: 100
      },
      inner: {
        left: (i.left - o.left) / (o.right - o.left) * 100,
        right: (i.right - o.left) / (o.right - o.left) * 100,
        top: (i.top - o.top) / (o.bottom - o.top) * 100,
        bottom: (i.bottom - o.top) / (o.bottom - o.top) * 100
      }
    };
  }
  // Maps a unit square into a convex quadrilateral, for inverse image resampling.
    function perspectiveMap(corners) {
    if (!Array.isArray(corners) || corners.length !== 4 || !corners.every(p => Number.isFinite(p.x) && Number.isFinite(p.y))) throw new Error('Place all four corners.');
    const cross = corners.map((a, n) => {
      const b = corners[(n + 1) % 4], c = corners[(n + 2) % 4];
      return (b.x - a.x) * (c.y - b.y) - (b.y - a.y) * (c.x - b.x);
    });
    if (!cross.every(v => v > .01)) throw new Error('Keep corners in order without crossing: top left, top right, bottom right, bottom left.');
    const [a, b, c, d] = corners;
    const dx1 = b.x - c.x, dx2 = d.x - c.x, dx3 = a.x - b.x + c.x - d.x;
    const dy1 = b.y - c.y, dy2 = d.y - c.y, dy3 = a.y - b.y + c.y - d.y;
    const det = dx1 * dy2 - dx2 * dy1;
    if (Math.abs(det) < 1e-8) throw new Error('Move the corners farther apart.');
    const g = (dx3 * dy2 - dx2 * dy3) / det, h = (dx1 * dy3 - dx3 * dy1) / det;
    return (u, v) => ({
      x: ((b.x - a.x + g * b.x) * u + (d.x - a.x + h * d.x) * v + a.x) / (g * u + h * v + 1),
      y: ((b.y - a.y + g * b.y) * u + (d.y - a.y + h * d.y) * v + a.y) / (g * u + h * v + 1)
    });
  }
  function canvas(width, height) {
    const result = document.createElement('canvas');
    result.width = Math.max(1, Math.round(width));
    result.height = Math.max(1, Math.round(height));
    return result;
  }
  async function readImage(url) {
    const image = new Image;
    image.src = url;
    await image.decode();
    return image;
  }
  async function imageFromFile(file) {
    if (!file || ![ 'image/jpeg', 'image/png', 'image/webp' ].includes(file.type)) throw new Error('Choose a JPG, PNG or WebP image.');
    if (file.size > MAX_BYTES) throw new Error('This image is too large. Choose a file under 15 MB.');
    const url = await new Promise((resolve, reject) => {
      const reader = new FileReader;
      reader.onload = () => resolve(reader.result);
      reader.onerror = () => reject(new Error('Could not read this photo.'));
      reader.readAsDataURL(file);
    });
    {
      const image = await readImage(url);
      if (Math.min(image.width, image.height) < 100 || Math.max(image.width, image.height) > 12e3) throw new Error('Use an image between 100 and 12,000 pixels on each side.');
      const scale = Math.min(1, 2e3 / Math.max(image.width, image.height));
      const output = canvas(image.width * scale, image.height * scale);
      output.getContext('2d').drawImage(image, 0, 0, output.width, output.height);
      return {
        image: output.toDataURL('image/png'),
        width: output.width,
        height: output.height
      };
    }
  }
  // Contrast peaks suggest straight borders. The user must review all eight lines.
    function detectGuides({data: data, width: width, height: height}) {
    const profile = vertical => {
      const length = vertical ? width : height, span = vertical ? height : width;
      const values = new Array(length).fill(0);
      for (let n = 2; n < length - 2; n++) {
        for (let j = Math.floor(span * .25); j < span * .75; j++) {
          const a = vertical ? (j * width + n - 1) * 4 : ((n - 1) * width + j) * 4;
          const b = vertical ? (j * width + n + 1) * 4 : ((n + 1) * width + j) * 4;
          values[n] += (Math.abs(data[a] - data[b]) + Math.abs(data[a + 1] - data[b + 1]) + Math.abs(data[a + 2] - data[b + 2])) / (span * .5 * 3);
        }
      }
      const peak = (lo, hi) => {
        let at = Math.max(2, Math.ceil(lo));
        for (let n = at; n <= Math.min(length - 3, hi); n++) if (values[n] > values[at]) at = n;
        return {
          at: at,
          strength: values[at]
        };
      };
      const left = peak(length * .015, length * .3), right = peak(length * .7, length * .985);
      if (Math.min(left.strength, right.strength) < 12) return null;
      const distance = right.at - left.at;
      const innerLeft = peak(left.at + Math.max(3, distance * .02), left.at + distance * .16);
      const innerRight = peak(right.at - distance * .16, right.at - Math.max(3, distance * .02));
      if (Math.min(innerLeft.strength, innerRight.strength) < 8) return null;
      return [ left.at, innerLeft.at, innerRight.at, right.at ].map(v => v / length * 100);
    };
    const x = profile(true), y = profile(false);
    if (!x || !y) return null;
    const result = {
      outer: {
        left: x[0],
        right: x[3],
        top: y[0],
        bottom: y[3]
      },
      inner: {
        left: x[1],
        right: x[2],
        top: y[1],
        bottom: y[2]
      }
    };
    return measureBorders(result) ? result : null;
  }
  async function rectifyImage(url, corners, ratio) {
    const map = perspectiveMap(corners);
    const image = await readImage(url), source = canvas(image.width, image.height);
    source.getContext('2d').drawImage(image, 0, 0);
    const src = source.getContext('2d').getImageData(0, 0, source.width, source.height);
    const height = Math.min(1600, image.height), output = canvas(height * ratio, height);
    const ctx = output.getContext('2d'), pixels = ctx.createImageData(output.width, output.height);
    for (let y = 0; y < output.height; y++) for (let x = 0; x < output.width; x++) {
      const p = map(x / (output.width - 1), y / (output.height - 1));
      const sx = clamp(p.x / 100 * (image.width - 1), 0, image.width - 1), sy = clamp(p.y / 100 * (image.height - 1), 0, image.height - 1);
      const ix = Math.floor(sx), iy = Math.floor(sy), fx = sx - ix, fy = sy - iy, out = (y * output.width + x) * 4;
      for (let c = 0; c < 4; c++) {
        const at = (dx, dy) => src.data[(Math.min(iy + dy, image.height - 1) * image.width + Math.min(ix + dx, image.width - 1)) * 4 + c];
        pixels.data[out + c] = at(0, 0) * (1 - fx) * (1 - fy) + at(1, 0) * fx * (1 - fy) + at(0, 1) * (1 - fx) * fy + at(1, 1) * fx * fy;
      }
    }
    ctx.putImageData(pixels, 0, 0);
    return {
      image: output.toDataURL('image/png'),
      width: output.width,
      height: output.height
    };
  }
  function savedRecords(mode, action) {
    return new Promise((resolve, reject) => {
      const open = indexedDB.open('pokget-measure', 1);
      open.onupgradeneeded = () => open.result.createObjectStore('cards', {
        keyPath: 'id'
      });
      open.onerror = () => reject(new Error('Local storage is unavailable. Export your measurement instead.'));
      open.onblocked = () => reject(new Error('Close other Measure tabs and try again.'));
      open.onsuccess = () => {
        const db = open.result, tx = db.transaction('cards', mode);
        let result;
        try {
          const request = action(tx.objectStore('cards'));
          request.onsuccess = () => {
            result = request.result;
          };
        } catch (error) {
          db.close();
          reject(error);
          return;
        }
        tx.oncomplete = () => {
          db.close();
          resolve(result);
        };
        tx.onabort = tx.onerror = () => {
          db.close();
          reject(new Error('Could not save locally. Storage may be full. Export your measurement instead.'));
        };
      };
    });
  }
  function download(blob, filename) {
    const url = URL.createObjectURL(blob), a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 3e4);
  }
  function createMeasure() {
    let observer, alive = true, pointers = new Map(), drag = null, editRevision = 0;
    return {
      side: 'front',
      sides: {
        front: emptySide(),
        back: emptySide()
      },
      group: 'outer',
      edge: 'left',
      tab: 'measure',
      zoom: 1,
      panX: 0,
      panY: 0,
      frameWidth: 500,
      frameHeight: 600,
      busy: false,
      error: '',
      notice: '',
      loadID: 0,
      perspective: false,
      corners: [],
      ratio: 63 / 88,
      angle: 0,
      grid: true,
      outerColor: '#65a9ff',
      innerColor: '#5ce0c6',
      title: '',
      tags: '',
      filter: '',
      saved: [],
      recordID: '',
      deleteID: '',
      storageError: '',
      dirty: false,
      imageURL: '',
      showURL: false,
      standards: STANDARDS,
      edges: EDGES,
      gradeScope: 'side',
      init() {
        this.$nextTick(() => {
          if (!alive) return;
          observer = new ResizeObserver(([entry]) => {
            this.frameWidth = entry.contentRect.width;
            this.frameHeight = entry.contentRect.height;
          });
          observer.observe(this.$refs.viewport);
        });
        this.refreshSaved();
      },
      destroy() {
        alive = false;
        this.loadID++;
        observer?.disconnect();
        pointers.clear();
      },
      get current() {
        return this.sides[this.side];
      },
      get metrics() {
        return this.current.image ? measureBorders(this.current.guides) : null;
      },
      get ready() {
        return Boolean(this.metrics && this.current.confirmed && !this.perspective);
      },
      get selectedValue() {
        return this.current.guides[this.group][this.edge];
      },
      get selectedLimits() {
        return guideLimits(this.current.guides, this.group, this.edge);
      },
      get stageStyle() {
        const fit = Math.min((this.frameWidth - 50) / (this.current.width || 1), (this.frameHeight - 50) / (this.current.height || 1));
        const w = Math.max(1, this.current.width * fit), h = Math.max(1, this.current.height * fit);
        return `display:${this.current.image ? 'block' : 'none'};width:${w}px;height:${h}px;left:${(this.frameWidth - w) / 2 + this.panX}px;top:${(this.frameHeight - h) / 2 + this.panY}px;transform:scale(${this.zoom});--guide-hit:${44 / this.zoom}px;--guide-line:${1.5 / this.zoom}px;--guide-font:${11 / this.zoom}px`;
      },
      get guideList() {
        return [ 'outer', 'inner' ].flatMap(group => EDGES.map(edge => ({
          id: group + '-' + edge,
          group: group,
          edge: edge,
          vertical: edge === 'left' || edge === 'right'
        })));
      },
      get filteredSaved() {
        const q = this.filter.toLowerCase().trim();
        return this.saved.filter(r => (r.title + ' ' + r.tags).toLowerCase().includes(q));
      },
      get bothReady() {
        return !this.perspective && SIDES.every(side => this.sides[side].image && this.sides[side].confirmed && measureBorders(this.sides[side].guides));
      },
      get grades() {
        return this.gradeRows(this.gradeScope);
      },
      gradeRows(scope) {
        const both = scope === 'both', ready = both ? this.bothReady : this.ready;
        return STANDARDS.map(s => {
          const result = !ready ? null : both ? gradeForBoth(s.company, measureBorders(this.sides.front.guides), measureBorders(this.sides.back.guides)) : gradeFor(s.company, this.metrics, this.side);
          const published = s.rows.some(row => both ? Number.isFinite(row.front) && Number.isFinite(row.back) : Number.isFinite(row[this.side]));
          return {
            ...s,
            result: result,
            display: result?.grade || '—',
            detail: !ready ? both ? 'Measure and confirm both sides' : 'Confirm the eight guides' : result?.label || (published ? 'Outside listed thresholds' : 'No published threshold')
          };
        });
      },
      format(value) {
        return Number.isFinite(value) ? `${value.toFixed(1)} / ${(100 - value).toFixed(1)}` : '— / —';
      },
      threshold(value) {
        return Number.isFinite(value) ? `${value}/${100 - value}` : 'Not published';
      },
      markChanged() {
        editRevision++;
        this.dirty = true;
        this.notice = '';
      },
      setSide(side) {
        if (!SIDES.includes(side) || this.busy) return;
        this.side = side;
        this.fit();
        this.perspective = false;
        this.error = '';
      },
      fit() {
        this.zoom = 1;
        this.panX = 0;
        this.panY = 0;
        pointers.clear();
        drag = null;
      },
      setZoom(value) {
        this.zoom = clamp(Number(value) || 1, 1, 15);
        if (this.zoom === 1) {
          this.panX = 0;
          this.panY = 0;
        }
      },
      setGuide(value) {
        if (this.busy || this.perspective || !this.current.image) return;
        // After initial review, deliberate manual edits update the live comparison.
        this.current.guides = moveGuide(this.current.guides, this.group, this.edge, value);
        this.markChanged();
      },
      nudge(amount) {
        this.setGuide(this.selectedValue + amount);
      },
      confirm() {
        if (this.metrics) {
          this.current.confirmed = true;
          this.markChanged();
          this.notice = 'Guides confirmed. Estimates now use your measurements.';
        }
      },
      resetGuides() {
        this.current.guides = clone(DEFAULT_GUIDES);
        this.current.confirmed = false;
        this.fit();
        this.markChanged();
      },
      lineStyle(line) {
        const position = this.current.guides[line.group][line.edge], color = line.group === 'outer' ? this.outerColor : this.innerColor;
        return `display:${this.perspective ? 'none' : 'block'};${line.vertical ? 'left' : 'top'}:${position}%;--guide-color:${color};pointer-events:${this.group === line.group && !this.perspective ? 'auto' : 'none'}`;
      },
      guideBounds(line) {
        return guideLimits(this.current.guides, line.group, line.edge);
      },
      point(event) {
        const r = this.$refs.stage.getBoundingClientRect();
        return {
          x: clamp((event.clientX - r.left) / r.width * 100, 0, 100),
          y: clamp((event.clientY - r.top) / r.height * 100, 0, 100)
        };
      },
      begin(event, line = null, corner = null) {
        if (this.busy || !this.current.image) return;
        if (line || corner !== null) event.currentTarget.focus?.({
          preventScroll: true
        });
        event.currentTarget.setPointerCapture?.(event.pointerId);
        pointers.set(event.pointerId, {
          x: event.clientX,
          y: event.clientY
        });
        if (pointers.size === 2) {
          const [a, b] = [ ...pointers.values() ];
          drag = {
            type: 'pinch',
            distance: Math.hypot(a.x - b.x, a.y - b.y),
            zoom: this.zoom,
            x: (a.x + b.x) / 2,
            y: (a.y + b.y) / 2,
            panX: this.panX,
            panY: this.panY
          };
          return;
        }
        if (line) {
          this.group = line.group;
          this.edge = line.edge;
          drag = {
            type: 'guide'
          };
        } else if (corner !== null) {
          drag = {
            type: 'corner',
            corner: corner
          };
        } else drag = {
          type: 'pan',
          x: event.clientX,
          y: event.clientY,
          panX: this.panX,
          panY: this.panY
        };
      },
      move(event) {
        if (!pointers.has(event.pointerId) || !drag) return;
        pointers.set(event.pointerId, {
          x: event.clientX,
          y: event.clientY
        });
        if (drag.type === 'pinch' && pointers.size === 2) {
          const [a, b] = [ ...pointers.values() ];
          this.setZoom(drag.zoom * Math.hypot(a.x - b.x, a.y - b.y) / Math.max(1, drag.distance));
          this.panX = drag.panX + (a.x + b.x) / 2 - drag.x;
          this.panY = drag.panY + (a.y + b.y) / 2 - drag.y;
        } else if (drag.type === 'guide') {
          const p = this.point(event);
          this.setGuide(this.edge === 'left' || this.edge === 'right' ? p.x : p.y);
        } else if (drag.type === 'corner') {
          this.corners[drag.corner] = this.point(event);
        } else if (drag.type === 'pan') {
          this.panX = drag.panX + event.clientX - drag.x;
          this.panY = drag.panY + event.clientY - drag.y;
        }
      },
      end(event) {
        pointers.delete(event.pointerId);
        drag = null;
      },
      async loadFile(file) {
        if (!file || this.busy) return;
        const id = ++this.loadID, side = this.side;
        this.busy = true;
        this.error = '';
        try {
          const image = await imageFromFile(file);
          if (!alive || id !== this.loadID) return;
          this.sides[side] = {
            ...emptySide(),
            ...image,
            original: image.image
          };
          this.perspective = false;
          this.fit();
          this.markChanged();
          this.notice = 'Photo ready. Align the card edges and design borders, then confirm.';
        } catch (error) {
          if (alive && id === this.loadID) this.error = error.message || 'This image could not be opened.';
        } finally {
          if (alive && id === this.loadID) this.busy = false;
        }
      },
      async loadURL() {
        if (this.busy) return;
        let url;
        try {
          url = new URL(this.imageURL);
          if (url.protocol !== 'https:' && url.origin !== location.origin || url.username || url.password) throw new Error;
        } catch {
          this.error = 'Enter a public HTTPS image URL.';
          return;
        }
        const id = ++this.loadID;
        this.busy = true;
        this.error = '';
        const image = new Image;
        image.crossOrigin = 'anonymous';
        image.referrerPolicy = 'no-referrer';
        let timer;
        try {
          image.src = url.href;
          await Promise.race([ image.decode(), new Promise((_, reject) => {
            timer = setTimeout(() => reject(new Error('timeout')), 2e4);
          }) ]);
          if (!alive || id !== this.loadID) return;
          if (Math.min(image.width, image.height) < 100 || Math.max(image.width, image.height) > 12e3) throw new Error('dimensions');
          const scale = Math.min(1, 2e3 / Math.max(image.width, image.height)), out = canvas(image.width * scale, image.height * scale);
          out.getContext('2d').drawImage(image, 0, 0, out.width, out.height);
          const data = out.toDataURL('image/png');
          this.sides[this.side] = {
            ...emptySide(),
            image: data,
            original: data,
            width: out.width,
            height: out.height
          };
          this.perspective = false;
          this.fit();
          this.markChanged();
          this.notice = 'Image imported. Align and confirm the eight guides.';
          this.showURL = false;
        } catch (error) {
          if (alive && id === this.loadID) this.error = error.message === 'dimensions' ? 'Use an image between 100 and 12,000 pixels on each side.' : 'Could not import this URL. The site may block image access. Download the photo and choose the file instead.';
        } finally {
          clearTimeout(timer);
          image.removeAttribute('src');
          if (alive && id === this.loadID) this.busy = false;
        }
      },
      async autoDetect() {
        if (!this.current.image || this.busy) return;
        this.busy = true;
        this.error = '';
        try {
          const image = await readImage(this.current.image);
          if (!alive) return;
          const scale = Math.min(1, 400 / Math.max(image.width, image.height)), small = canvas(image.width * scale, image.height * scale);
          const ctx = small.getContext('2d');
          ctx.drawImage(image, 0, 0, small.width, small.height);
          const guides = detectGuides(ctx.getImageData(0, 0, small.width, small.height));
          if (!guides) {
            this.notice = 'No clear borders found. Use the manual guides or correct perspective first.';
            return;
          }
          this.current.guides = guides;
          this.current.confirmed = false;
          this.markChanged();
          this.notice = 'Border suggestions ready. Check all eight lines before confirming.';
        } catch {
          this.error = 'Could not detect borders. Place the guides manually.';
        } finally {
          this.busy = false;
        }
      },
      startPerspective() {
        const o = this.current.guides.outer;
        this.corners = [ {
          x: o.left,
          y: o.top
        }, {
          x: o.right,
          y: o.top
        }, {
          x: o.right,
          y: o.bottom
        }, {
          x: o.left,
          y: o.bottom
        } ];
        this.perspective = true;
        this.error = '';
      },
      cornerNudge(index, axis, amount) {
        this.corners[index][axis] = clamp(this.corners[index][axis] + amount, 0, 100);
      },
      async applyPerspective() {
        if (this.busy) return;
        this.busy = true;
        this.error = '';
        try {
          const result = await rectifyImage(this.current.image, this.corners, Number(this.ratio));
          if (!alive) return;
          Object.assign(this.current, result);
          this.current.guides = {
            outer: {
              left: 0,
              right: 100,
              top: 0,
              bottom: 100
            },
            inner: {
              left: 5,
              right: 95,
              top: 5,
              bottom: 95
            }
          };
          this.current.confirmed = false;
          this.perspective = false;
          this.fit();
          this.markChanged();
          this.notice = 'Perspective corrected. Align the inner design borders again.';
        } catch (error) {
          this.error = error.message;
        } finally {
          this.busy = false;
        }
      },
      async rotate(degrees) {
        if (this.busy || !this.current.image) return;
        this.busy = true;
        this.error = '';
        try {
          const image = await readImage(this.current.image);
          if (!alive) return;
          const angle = Number(degrees) * Math.PI / 180, c = Math.abs(Math.cos(angle)), s = Math.abs(Math.sin(angle)), out = canvas(image.width * c + image.height * s, image.width * s + image.height * c), ctx = out.getContext('2d');
          ctx.translate(out.width / 2, out.height / 2);
          ctx.rotate(angle);
          ctx.drawImage(image, -image.width / 2, -image.height / 2);
          Object.assign(this.current, {
            image: out.toDataURL('image/png'),
            width: out.width,
            height: out.height
          });
          this.resetGuides();
          this.angle = 0;
          this.notice = 'Image rotated. Realign the measurement guides.';
        } catch {
          this.error = 'Could not rotate this image.';
        } finally {
          this.busy = false;
        }
      },
      async cropToEdges() {
        if (!this.metrics || this.busy || this.perspective) return;
        this.busy = true;
        this.error = '';
        try {
          const img = await readImage(this.current.image);
          if (!alive) return;
          const o = this.current.guides.outer, x = o.left / 100 * img.width, y = o.top / 100 * img.height, width = (o.right - o.left) / 100 * img.width, height = (o.bottom - o.top) / 100 * img.height;
          const out = canvas(width, height);
          out.getContext('2d').drawImage(img, x, y, width, height, 0, 0, out.width, out.height);
          Object.assign(this.current, {
            image: out.toDataURL('image/png'),
            width: out.width,
            height: out.height,
            guides: croppedGuides(this.current.guides)
          });
          this.fit();
          this.markChanged();
          this.notice = 'Cropped to the card edges. Measured ratios are preserved.';
        } catch {
          this.error = 'Could not crop this image.';
        } finally {
          this.busy = false;
        }
      },
      async restoreImage() {
        if (!this.current.original || this.busy) return;
        this.busy = true;
        try {
          const img = await readImage(this.current.original);
          if (!alive) return;
          Object.assign(this.current, {
            image: this.current.original,
            width: img.width,
            height: img.height
          });
          this.perspective = false;
          this.resetGuides();
        } catch {
          this.error = 'Could not restore the image.';
        } finally {
          this.busy = false;
        }
      },
      async refreshSaved() {
        try {
          const records = await savedRecords('readonly', store => store.getAll());
          if (alive) this.saved = records.filter(r => r?.version === 1 && typeof r.title === 'string' && typeof r.tags === 'string').sort((a, b) => b.updated - a.updated);
        } catch (error) {
          if (alive) this.storageError = error.message;
        }
      },
      async save() {
        if (!this.sides.front.image && !this.sides.back.image || this.busy) return;
        this.busy = true;
        this.error = '';
        const revision = editRevision;
        const id = this.recordID || crypto.randomUUID(), record = {
          version: 1,
          id: id,
          title: this.title.trim().slice(0, 100) || 'Untitled card',
          tags: this.tags.trim().slice(0, 200),
          updated: Date.now(),
          sides: clone(this.sides)
        };
        try {
          await savedRecords('readwrite', store => store.put(record));
          if (!alive) return;
          this.recordID = id;
          this.dirty = editRevision !== revision;
          this.notice = this.dirty ? 'Earlier version saved. Save again to keep your latest edits.' : 'Measurement saved in this browser.';
          await this.refreshSaved();
        } catch (error) {
          this.error = error.message;
        } finally {
          this.busy = false;
        }
      },
      openSaved(record) {
        if (this.busy) return;
        if (!SIDES.every(side => record.sides?.[side] && typeof record.sides[side].image === 'string' && (!record.sides[side].image || /^data:image\/(png|jpeg|webp);base64,/.test(record.sides[side].image) && measureBorders(record.sides[side].guides)))) {
          this.error = 'This saved measurement is damaged. Choose a new photo.';
          return;
        }
        this.sides = clone(record.sides);
        this.title = record.title;
        this.tags = record.tags;
        this.recordID = record.id;
        this.side = this.sides.front.image ? 'front' : 'back';
        this.dirty = false;
        this.tab = 'measure';
        this.perspective = false;
        this.error = '';
        this.notice = 'Saved measurement opened.';
        this.fit();
      },
      async deleteSaved(id) {
        try {
          await savedRecords('readwrite', store => store.delete(id));
          if (this.recordID === id) {
            this.recordID = '';
            this.dirty = true;
          }
          this.deleteID = '';
          await this.refreshSaved();
        } catch (error) {
          this.error = error.message;
        }
      },
      newCard() {
        if (this.busy) return;
        this.loadID++;
        this.sides = {
          front: emptySide(),
          back: emptySide()
        };
        this.side = 'front';
        this.recordID = '';
        this.title = '';
        this.tags = '';
        this.dirty = false;
        this.perspective = false;
        this.error = '';
        this.notice = '';
        this.fit();
      },
      exportJSON(history = false) {
        const records = history ? this.saved : [ {
          version: 1,
          title: this.title || 'Untitled card',
          tags: this.tags,
          sides: clone(this.sides)
        } ];
        download(new Blob([ JSON.stringify({
          app: 'Pokget Measure',
          version: 1,
          records: records
        }, null, 2) ], {
          type: 'application/json'
        }), history ? 'pokget-measure-history.json' : 'pokget-measurement.json');
      },
      async exportPNG() {
        if (!this.ready || this.busy) return;
        this.busy = true;
        this.error = '';
        try {
          const img = await readImage(this.current.image);
          if (!alive) return;
          const out = canvas(1e3, 1200), ctx = out.getContext('2d');
          ctx.fillStyle = '#171821';
          ctx.fillRect(0, 0, 1e3, 1200);
          ctx.fillStyle = '#ffffff';
          ctx.font = 'bold 30px system-ui';
          ctx.fillText((this.title || 'Pokget · Centering').slice(0, 50), 40, 55);
          ctx.font = '20px system-ui';
          ctx.fillText(this.side === 'front' ? 'Front measurement' : 'Back measurement', 40, 90);
          const scale = Math.min(920 / img.width, 740 / img.height), w = img.width * scale, h = img.height * scale, x = (1e3 - w) / 2, y = 120 + (740 - h) / 2;
          ctx.drawImage(img, x, y, w, h);
          for (const line of this.guideList) {
            ctx.strokeStyle = line.group === 'outer' ? this.outerColor : this.innerColor;
            ctx.lineWidth = 2;
            const pos = this.current.guides[line.group][line.edge] / 100;
            ctx.beginPath();
            if (line.vertical) {
              ctx.moveTo(x + w * pos, y);
              ctx.lineTo(x + w * pos, y + h);
            } else {
              ctx.moveTo(x, y + h * pos);
              ctx.lineTo(x + w, y + h * pos);
            }
            ctx.stroke();
          }
          ctx.fillStyle = '#ffffff';
          ctx.font = 'bold 28px monospace';
          ctx.fillText(`L/R ${this.format(this.metrics.lr)}     T/B ${this.format(this.metrics.tb)}`, 40, 910);
          ctx.font = '20px system-ui';
          this.gradeRows('side').forEach((r, i) => ctx.fillText(`${r.company}: ${r.display} · ${r.detail}`.slice(0, 44), 40 + i % 2 * 480, 960 + Math.floor(i / 2) * 45));
          ctx.fillStyle = '#c6c3cf';
          ctx.font = '17px system-ui';
          ctx.fillText('Centering estimate only. Corners, edges and surface are not assessed.', 40, 1135);
          ctx.fillText('Based on manually confirmed guides · pokget', 40, 1165);
          const blob = await new Promise(resolve => out.toBlob(resolve, 'image/png'));
          if (!blob) throw new Error;
          download(blob, `pokget-centering-${this.side}.png`);
          this.notice = 'Result image exported.';
        } catch {
          this.error = 'Could not export the result image.';
        } finally {
          this.busy = false;
        }
      }
    };
  }
  return {
    createMeasure: createMeasure,
    DEFAULT_GUIDES: DEFAULT_GUIDES,
    STANDARDS: STANDARDS,
    measureBorders: measureBorders,
    moveGuide: moveGuide,
    guideLimits: guideLimits,
    gradeFor: gradeFor,
    gradeForBoth: gradeForBoth,
    croppedGuides: croppedGuides,
    perspectiveMap: perspectiveMap,
    detectGuides: detectGuides,
    imageFromFile: imageFromFile
  };
});
