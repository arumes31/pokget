import { parse, parseFragment } from 'parse5';
import * as acorn from 'acorn';

export const normalize = text => text.replace(/\s+/g, ' ').trim();
const visible = text => /[A-Za-z]/.test(text) && !/^__GO_VALUE_\d+__$/.test(text);
const attributes = ['placeholder', 'aria-label', 'title', 'alt'];
const ignoredTags = new Set(['script', 'style']);

// These names/identifiers deliberately retain their spelling in either language.
export const invariant = new Set([
  'Pokget', 'Pokémon', 'Disney Lorcana', 'Magic: The Gathering', 'One Piece', 'Weiss Schwarz', 'Yu-Gi-Oh!',
  'PSA', 'BGS', 'CGC', 'SGC', 'TAG', 'ACE', 'TCG', 'EUR', 'USD', 'Euro',
  'English', 'Deutsch', 'NO. 001', 'NO. 018', 'NO. 032', 'POKGET ORIGINALS',
  'Shining Fates • SV107/SV122', 'https://…/card.jpg', '© 2026 Pokget',
  'TCGPlayer',
]);

function valueStrings(node) {
  if (!node) return [];
  if (node.type === 'Literal' && typeof node.value === 'string') return [node.value];
  if (node.type === 'ConditionalExpression') return [...valueStrings(node.consequent), ...valueStrings(node.alternate)];
  if (node.type === 'LogicalExpression') return [...valueStrings(node.left), ...valueStrings(node.right)];
  if (node.type === 'TemplateLiteral') return [node.quasis.map((part, i) => part.value.cooked + (i < node.expressions.length ? `{${i}}` : '')).join('')];
  if (node.type === 'BinaryExpression' && node.operator === '+') {
    const parts = [];
    const hasString = n => (n.type === 'Literal' && typeof n.value === 'string') || (n.type === 'BinaryExpression' && (hasString(n.left) || hasString(n.right)));
    function flatten(n) {
      if (n.type === 'BinaryExpression' && n.operator === '+' && hasString(n)) { flatten(n.left); flatten(n.right); }
      else parts.push(n.type === 'Literal' ? String(n.value) : null);
    }
    flatten(node);
    let index = 0;
    return [parts.map(part => part === null ? `{${index++}}` : part).join('')];
  }
  return [];
}

export function auditTemplate(source, filename, hasTranslation) {
  const failures = [];
  let valueIndex = 0;
  const html = source.replace(/{{[\s\S]*?}}/g, action => /^{{\s*(?:if\b|else\b|end\b|range\b|define\b|template\b|with\b|\/\*)/.test(action) ? '__GO_BRANCH__' : `__GO_VALUE_${valueIndex++}__`);
  const tree = /<!doctype/i.test(html) ? parse(html) : parseFragment(html);
  function check(text, marked, context) {
    if (text.includes('__GO_BRANCH__')) {
      text.split('__GO_BRANCH__').forEach(part => check(part, marked, context));
      return;
    }
    text = normalize(text);
    if (!visible(text) || invariant.has(text)) return;
    let index = 0;
    text = text.replace(/__GO_VALUE_\d+__/g, () => `{${index++}}`);
    if (!/[A-Za-z]/.test(text)) return;
    if (!marked) failures.push(`${filename}: unmarked ${context}: ${text}`);
    if (!hasTranslation(text)) failures.push(`${filename}: missing German ${context}: ${text}`);
  }
  function visit(node) {
    if (ignoredTags.has(node.tagName)) return;
    const attrs = Object.fromEntries((node.attrs || []).map(attr => [attr.name, attr.value]));
    const icon = /material-symbols/.test(attrs.class || '');
    const marked = Object.hasOwn(attrs, 'data-i18n') || Object.hasOwn(attrs, ':data-i18n');
    for (const child of node.childNodes || []) {
      if (child.nodeName === '#text' && !icon && !Object.hasOwn(attrs, 'x-text')) check(child.value, marked, 'text');
      else visit(child);
    }
    if (node.content) visit(node.content);
    for (const name of attributes) {
      if (attrs[name]) check(attrs[name], (attrs['data-i18n-attrs'] || '').split(' ').includes(name), name);
    }
    for (const name of ['x-text', ...attributes.map(attr => ':' + attr)]) {
      if (!attrs[name] || icon) continue;
      try {
        const expression = acorn.parseExpressionAt(attrs[name], 0, { ecmaVersion: 'latest' });
        for (const text of valueStrings(expression)) check(text, name === 'x-text' ? marked : (attrs['data-i18n-attrs'] || '').split(' ').includes(name.slice(1)), name);
      } catch (error) { failures.push(`${filename}: cannot audit ${name}: ${error.message}`); }
    }
    for (const [name, value] of Object.entries(attrs)) {
      if (name !== 'x-data' && !name.startsWith('@') && !name.startsWith('hx-on:') && !name.startsWith('x-on:')) continue;
      try {
        const script = name === 'x-data' ? `(${value})` : `async function handler(event, $event) { ${value} }`;
        failures.push(...auditRuntime(script, filename + ' ' + name, hasTranslation));
      } catch (error) { failures.push(`${filename}: cannot audit ${name}: ${error.message}`); }
    }
    if (node.tagName === 'option' && marked && !Object.hasOwn(attrs, 'value') && !Object.hasOwn(attrs, ':value')) failures.push(`${filename}: translated option needs an explicit value`);
  }
  visit(tree);
  return failures;
}

export function auditRuntime(source, filename, hasTranslation) {
  const failures = [];
  const tree = acorn.parse(source, { ecmaVersion: 'latest', sourceType: 'script' });
  const uiProperties = /^(?:error|notice|storageError|scanError|cameraError|scanStatus|navigationError|mutationError|addError|createError|reportsError|submitError|editError|autoNameError|label|note|detail|msg|message)$/;
  function check(node) {
    for (const value of valueStrings(node)) {
      const text = normalize(value);
      if (visible(text) && !invariant.has(text) && !hasTranslation(text)) failures.push(`${filename}: missing German runtime message: ${text}`);
    }
  }
  function visit(node) {
    if (!node || typeof node !== 'object') return;
    if (node.type === 'AssignmentExpression' && uiProperties.test(node.left.property?.name || node.left.name || '')) check(node.right);
    if (node.type === 'Property' && uiProperties.test(node.key.name || node.key.value || '')) check(node.value);
    if (node.type === 'VariableDeclarator' && uiProperties.test(node.id.name || '')) check(node.init);
    if (node.type === 'ReturnStatement') {
      for (const text of valueStrings(node.argument)) {
        if (/^[A-Z][a-z].*\s/.test(text)) check({ type: 'Literal', value: text });
      }
    }
    // These two internal sentinel errors are replaced by UI messages in catch handlers.
    if (node.type === 'NewExpression' && node.callee.name === 'Error' && !['timeout', 'dimensions'].includes(node.arguments[0]?.value)) check(node.arguments[0]);
    if (node.type === 'CallExpression') {
      const name = node.callee.property?.name || node.callee.name;
      if (name === 'notify' || name === 'setStatus') check(node.arguments[0]);
      if (name === 'showError' || name === 'setScanProgress') check(node.arguments[1]);
    }
    for (const value of Object.values(node)) {
      if (Array.isArray(value)) value.forEach(visit);
      else if (value && typeof value === 'object') visit(value);
    }
  }
  visit(tree);
  return failures;
}
