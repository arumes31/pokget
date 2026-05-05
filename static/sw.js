const CACHE_NAME = 'gettos-v1';
const ASSETS = [
  '/',
  '/static/css/styles.css',
  '/static/js/htmx.min.js',
  '/static/js/alpine.min.js',
  '/static/manifest.json'
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => {
      return cache.addAll(ASSETS);
    })
  );
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  // Skip non-GET requests and Auth routes
  if (event.request.method !== 'GET' || url.pathname.startsWith('/auth')) return;

  // Cache-first for static assets
  if (url.pathname.startsWith('/static/') || url.pathname.endsWith('.png') || url.pathname.endsWith('.css') || url.pathname.endsWith('.js')) {
    event.respondWith(
      caches.match(event.request).then((response) => {
        return response || fetch(event.request).then((fetchRes) => {
          return caches.open(CACHE_NAME).then((cache) => {
            cache.put(event.request, fetchRes.clone());
            return fetchRes;
          });
        });
      })
    );
    return;
  }

  // Network-first for everything else (HTML, API)
  event.respondWith(
    fetch(event.request)
      .then((response) => {
        const resClone = response.clone();
        caches.open(CACHE_NAME).then((cache) => {
          cache.put(event.request, resClone);
        });
        return response;
      })
      .catch(() => caches.match(event.request))
  );
});
