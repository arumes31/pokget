#!/usr/bin/env node
'use strict';

/**
 * Requests a CodeRabbit review for a pull request.
 *
 * Why this exists: CodeRabbit does not run automatic reviews on this repository
 * (it requires at least 10 stars), and the free plan allows a small number of
 * reviews per hour. Requesting one is therefore a deliberate step: only when the
 * current head commit has not been reviewed yet and the hourly quota is free.
 *
 * Usage:
 *   node scripts/coderabbit-review.mjs                 # PR of the current branch
 *   node scripts/coderabbit-review.mjs --pr 123        # specific PR
 *   node scripts/coderabbit-review.mjs --pr 123 --dry-run
 */

import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
export const REQUEST_BODY = '@coderabbitai review';
export const REQUEST_MARKER_PREFIX = '<!-- pokget-coderabbit-request:';

const QUOTA_MARKERS = [
  'rate limit',
  'review limit',
  'limit reached',
  'reached the limit',
  'you have reached',
  'reviews per hour',
  'per hour',
  'try again in',
];

export function isCodeRabbit(login) {
  return /coderabbit/i.test(String(login || ''));
}

export function requestMarker(headSha) {
  return `${REQUEST_MARKER_PREFIX}${headSha} -->`;
}

export function reviewCommit(review) {
  // `commit_id` is the shape returned by the REST reviews endpoint; `commit.oid`
  // is what gh's GraphQL view would expose. `gh pr view --json reviews` returns
  // neither, which is why reviews are fetched through `gh api`.
  return review?.commit_id || review?.commit?.oid || '';
}

/** True when a CodeRabbit review already covers the given head commit. */
export function reviewedHead(headSha, reviews) {
  return (reviews || []).some((review) => {
    if (!isCodeRabbit(review?.author?.login)) return false;
    // Without a commit hash we must assume it reviewed something recent.
    return reviewCommit(review) === '' || reviewCommit(review) === headSha;
  });
}

/** `--wait` only makes sense for an outstanding request or new request. */
export function shouldWait(action) {
  return action === 'request' || action === 'skip:already-requested';
}

/** True when the newest CodeRabbit comment reports an exhausted review quota. */
export function quotaExhausted(comments) {
  const newest = [...(comments || [])].reverse().find((comment) => isCodeRabbit(comment?.author?.login));
  if (!newest) return false;
  const body = String(newest.body || '').toLowerCase();
  return QUOTA_MARKERS.some((marker) => body.includes(marker));
}

/**
 * Pure decision function: `request`, or a `skip:<reason>` string.
 */
export function decideCodeRabbitAction({ state, isDraft, headSha, reviews, comments }) {
  if (String(state || '').toUpperCase() !== 'OPEN') return 'skip:closed';
  if (isDraft) return 'skip:draft';
  if (!headSha) return 'skip:unknown-head';
  if ((comments || []).some((comment) => String(comment?.body || '').includes(requestMarker(headSha)))) {
    return 'skip:already-requested';
  }
  if (reviewedHead(headSha, reviews)) return 'skip:already-reviewed';
  if (quotaExhausted(comments)) return 'skip:quota-exhausted';
  return 'request';
}

/**
 * Polls the pull request until CodeRabbit reviewed `headSha`.
 * Returns `reviewed`, `changes-requested`, `head-changed` or `timeout`.
 */
export async function waitForReview({
  headSha,
  fetchPr,
  timeoutMs = 600000,
  intervalMs = 15000,
  now = () => Date.now(),
  sleep = (ms) => new Promise((resolve) => { setTimeout(resolve, ms); }),
}) {
  const deadline = now() + timeoutMs;
  while (now() < deadline) {
    const pr = await fetchPr();
    if (pr?.headRefOid && pr.headRefOid !== headSha) return { status: 'head-changed' };
    const review = (pr?.reviews || []).find((entry) => {
      if (!isCodeRabbit(entry?.author?.login)) return false;
      const commit = reviewCommit(entry);
      return commit === '' || commit === headSha;
    });
    if (review) {
      return {
        status: String(review.state || '').toUpperCase() === 'CHANGES_REQUESTED' ? 'changes-requested' : 'reviewed',
        review,
      };
    }
    await sleep(intervalMs);
  }
  return { status: 'timeout' };
}

function gh(args, options = {}) {
  return execFileSync('gh', args, { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'], ...options });
}

function readFlag(argv, name) {
  const index = argv.indexOf(name);
  if (index === -1) return undefined;
  const value = argv[index + 1];
  return value && !value.startsWith('--') ? value : true;
}

async function main(argv) {
  const dryRun = argv.includes('--dry-run');
  const wait = argv.includes('--wait');
  const timeoutFlag = readFlag(argv, '--timeout');
  const timeoutSeconds = typeof timeoutFlag === 'string' ? Number(timeoutFlag) : 600;
  const timeoutMs = Number.isFinite(timeoutSeconds) && timeoutSeconds > 0 ? timeoutSeconds * 1000 : 600000;
  const prFlag = readFlag(argv, '--pr');
  const viewArgs = ['pr', 'view'];
  if (typeof prFlag === 'string') viewArgs.push(prFlag);
  viewArgs.push('--json', 'number,state,isDraft,headRefOid,comments');

  // `gh pr view --json reviews` omits the reviewed commit, so reviews come from
  // the REST endpoint, which exposes `commit_id`.
  const readPr = () => {
    const pr = JSON.parse(gh(viewArgs));
    const reviews = JSON.parse(gh(['api', `repos/{owner}/{repo}/pulls/${pr.number}/reviews?per_page=100`]));
    return {
      ...pr,
      reviews: (reviews || []).map((review) => ({
        author: { login: review?.user?.login },
        state: review?.state,
        commit_id: review?.commit_id,
      })),
    };
  };

  let pr;
  try {
    pr = readPr();
  } catch (error) {
    console.error(`cannot read pull request: ${error.stderr || error.message}`);
    return 2;
  }

  const action = decideCodeRabbitAction({
    state: pr.state,
    isDraft: pr.isDraft,
    headSha: pr.headRefOid,
    reviews: pr.reviews,
    comments: pr.comments,
  });

  if (action !== 'request') {
    console.log(`PR #${pr.number}: ${action}`);
    if (!wait || !shouldWait(action)) return 0;
  } else if (dryRun) {
    console.log(`PR #${pr.number}: would request a CodeRabbit review for ${pr.headRefOid}`);
    return 0;
  } else {
    const body = `${REQUEST_BODY}\n\n${requestMarker(pr.headRefOid)}`;
    try {
      gh(['pr', 'comment', String(pr.number), '--body', body]);
    } catch (error) {
      console.error(`cannot post the review request: ${error.stderr || error.message}`);
      return 2;
    }
    console.log(`PR #${pr.number}: requested a CodeRabbit review for ${pr.headRefOid}`);
  }

  if (!wait) return 0;

  const result = await waitForReview({
    headSha: pr.headRefOid,
    timeoutMs,
    fetchPr: readPr,
  });
  console.log(`PR #${pr.number}: coderabbit=${result.status}`);
  if (result.status === 'timeout') return 3;
  if (result.status === 'changes-requested' || result.status === 'head-changed') return 1;
  return 0;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exitCode = await main(process.argv.slice(2));
}
