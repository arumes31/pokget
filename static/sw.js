const CACHE_NAME = 'pokget-v2';
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

    // Skip non-GET requests and Auth routes
    if (event.request.method !== 'GET' || url.pathname.startsWith('/auth')) return;

    // Stale-while-revalidate for static assets
    if (url.pathname.startsWith('/static/') || url.pathname.endsWith('.png') || url.pathname.endsWith('.css') || url.pathname.endsWith('.js')) {
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

    // Network-first for everything else (HTML, API)
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
                return new Response('Offline', { status: 503 });
            }))
    );
});
