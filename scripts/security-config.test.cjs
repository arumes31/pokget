'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const projectRoot = path.resolve(__dirname, '..');
const read = (relativePath) => fs.readFileSync(path.join(projectRoot, relativePath), 'utf8');

test('rolling counters render configurable labels as text', () => {
  const vault = read('static/js/vault.js');

  assert.match(vault, /obj\.textContent = prefix \+ value\.toLocaleString\(\) \+ suffix;/);
  assert.doesNotMatch(vault, /obj\.innerHTML = prefix/);
});

test('automated workflows declare least-privilege repository access', () => {
  for (const workflow of ['.github/workflows/pipeline.yml', '.github/workflows/license.yml']) {
    const source = read(workflow);
    assert.match(source, /^permissions:\r?\n  contents: read$/m, workflow);
  }
});

test('the published image ships minified and validated JavaScript', () => {
  // The Dockerfile builds the assets itself; a host-side dist/ never reaches the image.
  const dockerfile = read('Dockerfile');
  assert.match(dockerfile, /RUN npm run build:static && npm run check:static/);
  assert.match(dockerfile, /COPY --chown=pokget:pokget --from=static-assets \/app\/dist\/static \.\/static/);
  assert.match(read('.dockerignore'), /^dist\r?$/m);
  const workflow = read('.github/workflows/pipeline.yml');
  const dockerJob = workflow.slice(workflow.indexOf('\n  docker:'));
  assert.match(dockerJob, /steps\.source\.outputs\.ref_name != 'latest'/);
});

test('repository default CodeQL setup is not duplicated by an advanced workflow', () => {
  assert.equal(
    fs.existsSync(path.join(projectRoot, '.github/workflows/codeql.yml')),
    false,
    'default and advanced CodeQL setups cannot upload analyses for the same repository',
  );
});

test('example.env documents every Compose interpolation without real credentials', () => {
  const example = read('example.env');
  const keys = new Set([...example.matchAll(/^([A-Z][A-Z0-9_]*)=/gm)].map(match => match[1]));
  for (const file of ['docker-compose.yml', 'docker-compose.ghcr.yml']) {
    for (const match of read(file).matchAll(/(?<!\$)\$\{([A-Z][A-Z0-9_]*)/g)) {
      assert.ok(keys.has(match[1]), `${file}: ${match[1]} missing from example.env`);
    }
  }
  assert.match(example, /^SESSION_KEY=$/m);
  assert.match(example, /^DB_PASSWORD=$/m);
  assert.match(example, /^LLM_API=$/m);
  assert.doesNotMatch(example, /SCAN_OCR_POOL_SIZE|SCAN_VISION_OCR_/);
});

test('both Compose variants expose the same application configuration', () => {
  const appEnvironment = file => read(file).split('    environment:')[1].split('    depends_on:')[0];
  const source = appEnvironment('docker-compose.yml');
  assert.equal(appEnvironment('docker-compose.ghcr.yml'), source);
  for (const key of ['SECURE_COOKIES', 'WRITE_TIMEOUT', 'SCAN_MAX_CONCURRENT', 'SCAN_PHASH_HIGH_CONF', 'SCAN_PHASH_POTENTIAL', 'CATALOG_WEISS_MAX_PAGES',
    'PRICE_BULK_INTERVAL_HOURS', 'PRICE_TRACKED_LIMIT', 'PRICE_BACKFILL_BATCH', 'PRICE_BACKFILL_REQUESTS_PER_SECOND', 'PRICE_EUR_FALLBACK']) {
    assert.ok(source.includes(`${key}=\${${key}:-`), `${key} is not configurable through Compose`);
  }
});

test('authenticated shell disables HTMX history cache to prevent sensitive snapshot persistence', () => {
  const indexHtml = read('templates/index.html');
  assert.match(indexHtml, /hx-history="false"/);
  assert.match(indexHtml, /localStorage\.removeItem\('htmx-history-cache'\)/);

  const vault = read('static/js/vault.js');
  assert.match(vault, /localStorage\.removeItem\('htmx-history-cache'\)/);

  const auth = read('static/js/auth.js');
  assert.match(auth, /localStorage\.removeItem\('htmx-history-cache'\)/);
});
