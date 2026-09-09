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

test('GHCR publishing minifies and validates JavaScript before building the image', () => {
  const workflow = read('.github/workflows/pipeline.yml');
  const dockerJob = workflow.slice(workflow.indexOf('\n  docker:'));
  const minifyStep = dockerJob.indexOf('name: Minify and validate JavaScript before image build');
  const imageBuild = dockerJob.indexOf('uses: docker/build-push-action@');

  assert.ok(minifyStep >= 0, 'docker job is missing the production asset step');
  assert.ok(imageBuild > minifyStep, 'image build must run after asset minification');
  assert.match(dockerJob, /npm run build:static/);
  assert.match(dockerJob, /npm run check:static/);
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
  assert.match(example, /^SCAN_VISION_OCR_ENABLED=false$/m);
});

test('both Compose variants expose the same application configuration', () => {
  const appEnvironment = file => read(file).split('    environment:')[1].split('    depends_on:')[0];
  const source = appEnvironment('docker-compose.yml');
  assert.equal(appEnvironment('docker-compose.ghcr.yml'), source);
  for (const key of ['SECURE_COOKIES', 'WRITE_TIMEOUT', 'SCAN_OCR_POOL_SIZE', 'SCAN_PHASH_HIGH_CONF', 'SCAN_PHASH_POTENTIAL', 'CATALOG_WEISS_MAX_PAGES']) {
    assert.ok(source.includes(`${key}=\${${key}:-`), `${key} is not configurable through Compose`);
  }
});
