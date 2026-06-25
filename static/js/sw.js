const CACHE_NAME = 'pokget-v2';

const ASSETS = [
  '/',
  '/dashboard',
  '/static/css/styles.css',
  '/static/js/htmx.min.js',
  '/static/js/alpine.min.js',
  '/static/js/vault.js',
  '/static/img/logo.png'
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => {
      return cache.addAll(ASSETS);
    })
  );
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((cacheNames) => {
      return Promise.all(
        cacheNames.filter((name) => name !== CACHE_NAME).map((name) => caches.delete(name))
      );
    })
  );
  self.clients.claim();
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  if (event.request.method !== 'GET') return;

  // Stale-while-revalidate for static assets
  if (url.pathname.startsWith('/static/')) {
    event.respondWith(
      caches.open(CACHE_NAME).then((cache) => {
        return cache.match(event.request).then((cachedResponse) => {
          const fetchPromise = fetch(event.request).then((networkResponse) => {
            if (networkResponse.ok) {
              cache.put(event.request, networkResponse.clone());
            }
            return networkResponse;
          }).catch(() => cachedResponse || new Response('Offline', { status: 503 }));

          return cachedResponse || fetchPromise;
        });
      })
    );
    return;
  }

  // Network-first for HTML and API requests
  event.respondWith(
    fetch(event.request)
      .then((response) => {
        if (response.ok) {
          const resClone = response.clone();
          caches.open(CACHE_NAME).then((cache) => {
            cache.put(event.request, resClone);
          });
        }
        return response;
      })
      .catch(() => caches.match(event.request).then((cached) => {
        if (cached) return cached;
        if (event.request.mode === 'navigate') return caches.match('/dashboard');
        return new Response('Offline', { status: 503 });
      }))
  );
});
