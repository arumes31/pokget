import assert from 'node:assert/strict';
import test from 'node:test';

import {
  decideCodeRabbitAction,
  quotaExhausted,
  requestMarker,
  reviewedHead,
} from './coderabbit-review.mjs';

const HEAD = 'a'.repeat(40);
const comment = (body, login = 'coderabbitai[bot]') => ({ author: { login }, body });
const review = (oid, login = 'coderabbitai[bot]') => ({ author: { login }, commit: { oid } });

test('an unreviewed open pull request gets a review request', () => {
  assert.equal(
    decideCodeRabbitAction({ state: 'OPEN', isDraft: false, headSha: HEAD, reviews: [], comments: [] }),
    'request',
  );
});

test('drafts, closed pull requests, and unknown heads are skipped', () => {
  assert.equal(decideCodeRabbitAction({ state: 'MERGED', isDraft: false, headSha: HEAD }), 'skip:closed');
  assert.equal(decideCodeRabbitAction({ state: 'OPEN', isDraft: true, headSha: HEAD }), 'skip:draft');
  assert.equal(decideCodeRabbitAction({ state: 'OPEN', isDraft: false, headSha: '' }), 'skip:unknown-head');
});

test('a review for the current head is never requested twice', () => {
  assert.equal(reviewedHead(HEAD, [review(HEAD)]), true);
  assert.equal(reviewedHead(HEAD, [review('b'.repeat(40))]), false);
  assert.equal(reviewedHead(HEAD, [review(HEAD, 'human-reviewer')]), false);
  assert.equal(
    decideCodeRabbitAction({ state: 'OPEN', isDraft: false, headSha: HEAD, reviews: [review(HEAD)], comments: [] }),
    'skip:already-reviewed',
  );
});

test('an existing request marker for the head suppresses duplicates', () => {
  const comments = [comment(`${requestMarker(HEAD)}`)];
  assert.equal(
    decideCodeRabbitAction({ state: 'OPEN', isDraft: false, headSha: HEAD, reviews: [], comments }),
    'skip:already-requested',
  );
  assert.equal(requestMarker(HEAD).includes(HEAD), true);
});

test('an exhausted hourly quota blocks new requests until CodeRabbit reports again', () => {
  const exhausted = [comment('You have reached the review limit for this hour. Try again in 42 minutes.')];
  assert.equal(quotaExhausted(exhausted), true);
  assert.equal(
    decideCodeRabbitAction({ state: 'OPEN', isDraft: false, headSha: HEAD, reviews: [], comments: exhausted }),
    'skip:quota-exhausted',
  );
  const recovered = [...exhausted, comment('Reviewing files...')];
  assert.equal(quotaExhausted(recovered), false);
  assert.equal(
    decideCodeRabbitAction({ state: 'OPEN', isDraft: false, headSha: HEAD, reviews: [], comments: recovered }),
    'request',
  );
});

test('human comments never trigger the quota or review heuristics', () => {
  const comments = [comment('review limit reached', 'some-human')];
  assert.equal(quotaExhausted(comments), false);
  assert.equal(reviewedHead(HEAD, [review(HEAD, 'some-human')]), false);
});
