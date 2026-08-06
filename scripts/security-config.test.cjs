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
