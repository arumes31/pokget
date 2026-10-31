import assert from 'node:assert/strict';
import test from 'node:test';

import {
  decideCodeRabbitAction,
  quotaExhausted,
  requestMarker,
  reviewCommit,
  reviewedHead,
  shouldWait,
  waitForReview,
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

test('reviewCommit prefers the REST commit_id used by gh api', () => {
  assert.equal(reviewCommit({ commit_id: HEAD, commit: { oid: 'b'.repeat(40) } }), HEAD);
  assert.equal(reviewCommit({ commit: { oid: HEAD } }), HEAD);
  assert.equal(reviewCommit({}), '');
});

test('only an outstanding request keeps --wait polling', () => {
  assert.equal(shouldWait('request'), true);
  assert.equal(shouldWait('skip:already-requested'), true);
  assert.equal(shouldWait('skip:quota-exhausted'), false);
  assert.equal(shouldWait('skip:already-reviewed'), false);
  assert.equal(shouldWait('skip:closed'), false);
  assert.equal(shouldWait('skip:draft'), false);
  assert.equal(shouldWait('skip:unknown-head'), false);
});

function fakeClock() {
  let ticks = 0;
  return {
    now: () => ticks * 1000,
    sleep: async () => { ticks += 1; },
  };
}

test('waitForReview resolves once CodeRabbit reviewed the head commit', async () => {
  const clock = fakeClock();
  const responses = [
    { headRefOid: HEAD, reviews: [] },
    { headRefOid: HEAD, reviews: [review(HEAD)] },
  ];
  const result = await waitForReview({
    headSha: HEAD,
    timeoutMs: 60000,
    fetchPr: async () => responses.shift(),
    ...clock,
  });
  assert.equal(result.status, 'reviewed');
});

test('waitForReview surfaces a requested change', async () => {
  const clock = fakeClock();
  const result = await waitForReview({
    headSha: HEAD,
    timeoutMs: 60000,
    fetchPr: async () => ({ headRefOid: HEAD, reviews: [{ ...review(HEAD), state: 'CHANGES_REQUESTED' }] }),
    ...clock,
  });
  assert.equal(result.status, 'changes-requested');
});

test('waitForReview stops when the head commit moved', async () => {
  const clock = fakeClock();
  const result = await waitForReview({
    headSha: HEAD,
    timeoutMs: 60000,
    fetchPr: async () => ({ headRefOid: 'b'.repeat(40), reviews: [] }),
    ...clock,
  });
  assert.equal(result.status, 'head-changed');
});

test('waitForReview times out when CodeRabbit never answers', async () => {
  const clock = fakeClock();
  const result = await waitForReview({
    headSha: HEAD,
    timeoutMs: 2500,
    fetchPr: async () => ({ headRefOid: HEAD, reviews: [] }),
    ...clock,
  });
  assert.equal(result.status, 'timeout');
});
