// A presentation timeline after the server has saved the authenticated session.
(() => {
    if (document.pokgetPackTransition) return;
    document.pokgetPackTransition = true;
    let active = null;
    const motion = matchMedia('(prefers-reduced-motion: reduce)');
    const stages = [[0, 'login-success'], [400, 'pack-enter'], [1200, 'pack-tension'],
        [1800, 'pack-tear'], [2500, 'cards-emerge'], [3200, 'card-select'],
        [3550, 'card-reveal'], [4600, 'transition-out']];
    const quietStages = [[0, 'login-success'], [150, 'pack-enter'], [350, 'pack-tear'], [550, 'card-reveal']];

    function start(form, destination, replace) {
        const template = document.querySelector('#auth-pack-template');
        const root = form.closest('.auth-experience');
        // A failed/missing enhancement must leave HTMX's normal redirect intact.
        if (!template || !root || !Element.prototype.animate) return false;
        const overlay = template.content.firstElementChild.cloneNode(true);
        const reduced = motion.matches;
        const duration = reduced ? 900 : 5000;
        const timeline = reduced ? quietStages : stages;
        const atlas = getComputedStyle(root).getPropertyValue('--card-atlas');
        if (atlas) overlay.style.setProperty('--card-atlas', atlas);
        overlay.classList.toggle('pack-reduced', reduced);
        let frame, deadline, completed = false;
        const started = performance.now();
        const wasInert = root.inert;
        const events = new AbortController();
        const options = { signal: events.signal };

        function cleanup() {
            cancelAnimationFrame(frame);
            clearTimeout(deadline);
            events.abort();
            overlay.remove();
            root.inert = wasInert;
            root.classList.remove('auth-entering');
            document.documentElement.classList.remove('pack-playing');
            delete form.dataset.authComplete;
            active = null;
        }
        function finish(skip = false) {
            if (completed) return;
            completed = true;
            cancelAnimationFrame(frame);
            clearTimeout(deadline);
            // The browser carries the final frame into the destination when supported.
            document.pokgetPackNavigation = !skip && !reduced;
            overlay.dataset.stage = 'complete';
            if (replace) location.replace(destination);
            else location.assign(destination);
        }
        function tick(now) {
            const elapsed = now - started;
            overlay.dataset.stage = timeline.findLast(([at]) => elapsed >= at)?.[1] || 'login-success';
            const crossDocument = 'onpageswap' in window && 'onpagereveal' in window;
            if (elapsed >= (crossDocument && !reduced ? 4850 : duration)) finish();
            else frame = requestAnimationFrame(tick);
        }
        document.body.append(overlay);
        if (getComputedStyle(overlay).getPropertyValue('--pack-ready').trim() !== '1') {
            overlay.remove();
            return false;
        }
        active = { form, cleanup, finish };
        form.dataset.authComplete = 'true';
        form.setAttribute('aria-busy', 'false');
        root.classList.add('auth-entering');
        document.documentElement.classList.add('pack-playing');
        overlay.focus({ preventScroll: true });
        root.inert = true;
        overlay.addEventListener('keydown', (event) => {
            if (event.key === 'Escape') { event.preventDefault(); finish(true); }
            if (event.key === 'Tab') event.preventDefault();
        }, options);
        document.addEventListener('visibilitychange', () => { if (document.hidden) finish(true); }, options);
        motion.addEventListener('change', () => { if (motion.matches) finish(true); }, options);
        window.addEventListener('pagehide', cleanup, options);
        // rAF may be throttled or unavailable in background tabs; navigation never depends on it.
        deadline = setTimeout(() => finish(), duration);
        frame = requestAnimationFrame(tick);
        if (document.hidden) finish(true);
        return true;
    }

    document.addEventListener('htmx:beforeOnLoad', (event) => {
        const form = event.detail.elt?.closest('form[data-auth-form]');
        const xhr = event.detail.xhr;
        if (!form || form.getAttribute('hx-post') !== '/auth/login' || xhr.status < 200 || xhr.status >= 300) return;
        const destination = xhr.getResponseHeader('HX-Redirect');
        if (!destination) return;
        if (active) { event.preventDefault(); return; }
        let url;
        try { url = new URL(destination, location.href); } catch { return; }
        // Only enhance an ordinary same-origin authenticated navigation.
        if (url.origin !== location.origin || !['http:', 'https:'].includes(url.protocol)) return;
        const replaceHeader = xhr.getResponseHeader('HX-Replace-Url');
        try {
            if (start(form, destination, !!replaceHeader && replaceHeader !== 'false')) event.preventDefault();
        } catch {
            active?.cleanup();
            // No cancellation: the original HTMX redirect still proceeds.
        }
    });
    document.addEventListener('htmx:beforeRequest', (event) => {
        if (active && event.detail.elt?.closest('form[data-auth-form]')) event.preventDefault();
    });
    document.addEventListener('htmx:beforeCleanupElement', (event) => {
        if (active && event.detail.elt.contains(active.form)) active.cleanup();
    });
})();
