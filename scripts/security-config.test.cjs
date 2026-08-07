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
