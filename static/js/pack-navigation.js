// Cross-document blending is reserved for the completed login presentation.
(() => {
    if (document.pokgetPackNavigationReady) return;
    document.pokgetPackNavigationReady = true;
    window.addEventListener('pageswap', (event) => {
        if (!document.pokgetPackNavigation) event.viewTransition?.skipTransition();
    });
    window.addEventListener('pagereveal', (event) => {
        if (!event.viewTransition) return;
        const from = window.navigation?.activation?.from?.url;
        if (!from || new URL(from).pathname !== '/auth' || matchMedia('(prefers-reduced-motion: reduce)').matches) {
            event.viewTransition.skipTransition();
        }
    });
})();
