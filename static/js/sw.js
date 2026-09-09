'use strict';

const CACHE_PREFIX = 'pokget-';
const CACHE_NAME = `${CACHE_PREFIX}shell-v14`;
const OFFLINE_URL = '/static/offline.html';
const PRECACHE_URLS = [
  OFFLINE_URL,
  '/static/fonts/material-symbols-ui.woff2',
  '/static/css/tailwind.css',
  '/static/css/styles.css',
  '/static/js/htmx.min.js',
  '/static/js/theme.js',
  '/static/js/i18n.js',
  '/static/js/alpine.min.js',
  '/static/js/vault.js',
  '/static/js/dialogs.js',
  '/static/js/scanner.js',
  '/static/js/device-ocr.js',
  '/static/js/measure.js',
  '/static/css/measure.css',
  '/static/img/logo-128.webp',
  '/static/img/favicon-32.png',
  '/static/img/icon-192.png',
  '/static/img/icon-512.png',
  '/static/manifest.json',
];

self.addEventListener('install', (event) => {
  event.waitUntil((async () => {
    const cache = await caches.open(CACHE_NAME);
    await cache.addAll(PRECACHE_URLS);
    await self.skipWaiting();
  })());
});

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const cacheNames = await caches.keys();
    await Promise.all(cacheNames
      .filter((name) => name.startsWith(CACHE_PREFIX) && name !== CACHE_NAME)
      .map((name) => caches.delete(name)));
    await self.clients.claim();
  })());
});

self.addEventListener('message', (event) => {
  if (event.data?.type === 'SKIP_WAITING') self.skipWaiting();
});

self.addEventListener('fetch', (event) => {
  const { request } = event;
  const url = new URL(request.url);
  if (request.method !== 'GET' || url.origin !== self.location.origin) return;

  if (request.mode === 'navigate') {
    event.respondWith(fetch(request).catch(async () => {
      const cache = await caches.open(CACHE_NAME);
      return cache.match(OFFLINE_URL)
        || new Response('Pokget is offline.', {
          status: 503,
          headers: { 'Content-Type': 'text/plain; charset=utf-8' },
        });
    }));
    return;
  }

  if (!url.pathname.startsWith('/static/')) return;

  const cacheKey = url.pathname;
  const cachePromise = caches.open(CACHE_NAME);
  const networkPromise = fetch(request).then(async (response) => {
    if (response.ok) {
      const cache = await cachePromise;
      await cache.put(cacheKey, response.clone());
    }
    return response;
  });

  event.waitUntil(networkPromise.then(() => undefined).catch(() => undefined));
  event.respondWith((async () => {
    const cache = await cachePromise;
    const cached = await cache.match(cacheKey);
    if (cached) return cached;
    try {
      return await networkPromise;
    } catch {
      return new Response('Static asset unavailable while offline.', {
        status: 503,
        headers: { 'Content-Type': 'text/plain; charset=utf-8' },
      });
    }
  })());
});
