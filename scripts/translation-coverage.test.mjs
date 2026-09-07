import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';
import { auditTemplate, auditRuntime } from './translation-coverage.mjs';

const source = fs.readFileSync('static/js/i18n.js', 'utf8');
const german = JSON.parse(source.match(/const german = (\{[\s\S]*?\n  \});/)[1]);
const patterns = JSON.parse(source.match(/const germanPatterns = (\{[\s\S]*?\n  \});/)?.[1] || '{}');
const hasTranslation = key => typeof (german[key] ?? patterns[key]) === 'string' && (german[key] ?? patterns[key]).trim().length > 0;

test('every interface label and accessible description has a German translation and marker', () => {
  const files = fs.readdirSync('templates').filter(name => name.endsWith('.html')).map(name => 'templates/' + name);
  files.push('static/offline.html');
  const failures = files.flatMap(file => auditTemplate(fs.readFileSync(file, 'utf8'), file, hasTranslation));
  assert.deepEqual(failures, [], failures.join('\n'));
});

test('authored JavaScript runtime messages have German translations', () => {
  const files = fs.readdirSync('static/js').filter(name => name.endsWith('.js') && !name.endsWith('.min.js') && name !== 'i18n.js');
  const failures = files.flatMap(name => auditRuntime(fs.readFileSync('static/js/' + name, 'utf8'), name, hasTranslation));
  assert.deepEqual(failures, [], failures.join('\n'));
});

test('coverage guard rejects a new untranslated label, an unmarked label, and a runtime error', () => {
  assert.match(auditTemplate('<button data-i18n>A brand new action</button>', 'fixture', hasTranslation).join('\n'), /missing German/);
  assert.match(auditTemplate('<button>Save</button>', 'fixture', hasTranslation).join('\n'), /unmarked text/);
  assert.match(auditRuntime("this.error = 'A newly introduced failure';", 'fixture', hasTranslation).join('\n'), /missing German runtime/);
  assert.deepEqual(auditTemplate('<span>{{ .Card.Name }}</span><span class="material-symbols-outlined">search</span>', 'fixture', hasTranslation), []);
});

test('literal HTTP errors and notifications returned by handlers have German translations', () => {
  const failures = [];
  for (const file of fs.readdirSync('internal/handlers').filter(name => name.endsWith('.go') && !name.endsWith('_test.go'))) {
    const source = fs.readFileSync('internal/handlers/' + file, 'utf8');
    for (const match of source.matchAll(/http\.Error\(w,\s*("(?:\\.|[^"\\])*")|"(?:msg|Message)":\s*("(?:\\.|[^"\\])*")/g)) {
      const text = JSON.parse(match[1] || match[2]);
      if (!hasTranslation(text)) failures.push(`${file}: missing German response: ${text}`);
    }
  }
  assert.deepEqual(failures, [], failures.join('\n'));
});

test('translated options retain submitted values and placeholders match across languages', () => {
  assert.match(auditTemplate('<option data-i18n>Raw</option>', 'fixture', hasTranslation).join('\n'), /explicit value/);
  for (const [english, german] of Object.entries(patterns)) {
    assert.deepEqual(german.match(/\{\d+\}/g)?.sort(), english.match(/\{\d+\}/g)?.sort(), english);
  }
});
