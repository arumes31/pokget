const CACHE_NAME = 'pokget-v1';
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
});

self.addEventListener('fetch', (event) => {
  event.respondWith(
    caches.match(event.request).then((response) => {
      return response || fetch(event.request).catch(() => {
        if (event.request.mode === 'navigate') {
          return caches.match('/dashboard');
        }
      });
    })
  );
});
