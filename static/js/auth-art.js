// Decorative artwork only. No authentication state or persistent storage.
(() => {
    if (document.pokgetFoilArt) return;
    document.pokgetFoilArt = true;

    const collections = [
        { id: 'celestial', names: ['Tidal prism', 'Aurora shard', 'Solstice'] },
        { id: 'botanical', names: ['Moonlit sanctuary', 'Ivory guardian', 'Orchid dusk'] },
        { id: 'mythic', names: ['Tide keeper', 'Moonfire phoenix', 'Gilded sentinel'] },
    ];
    const reduced = matchMedia('(prefers-reduced-motion: reduce)');
    const finePointer = matchMedia('(hover: hover) and (pointer: fine)');
    const scenes = new Map();
    const asset = (index) => `/static/img/auth/${collections[index].id}.webp`;
    const load = (index) => new Promise((resolve) => {
        const image = new Image();
        image.onload = () => resolve(true);
        image.onerror = () => resolve(false);
        image.src = asset(index);
    });

    function initialize(scene) {
        if (scenes.has(scene)) return;
        const root = scene.closest('.auth-experience');
        const events = new AbortController();
        const options = { signal: events.signal };
        let index = Math.floor(Math.random() * collections.length);
        let timer, transition, frame;
        let engaged = root.contains(document.activeElement);
        let visible = true;
        let disposed = false;
        let hovered = false;

        function apply() {
            root.style.setProperty('--card-atlas', `url("${asset(index)}")`);
            root.dataset.cardDesign = collections[index].id;
            scene.querySelectorAll('[data-card-name]').forEach((label) => {
                label.textContent = collections[index].names[Number(label.dataset.cardName)];
            });
        }
        function schedule() {
            clearTimeout(timer);
            const paused = engaged || reduced.matches || !visible || document.hidden || hovered;
            root.dataset.artPaused = String(paused);
            if (paused) {
                clearTimeout(transition);
                scene.classList.remove('is-changing');
            }
            if (!paused && !disposed) timer = setTimeout(advance, 14000);
        }
        async function advance() {
            const next = (index + 1) % collections.length;
            if (!await load(next) || disposed) return schedule();
            if (engaged || reduced.matches || !visible || document.hidden || hovered) return schedule();
            root.style.setProperty('--card-next-atlas', `url("${asset(next)}")`);
            scene.classList.add('is-changing');
            transition = setTimeout(() => {
                index = next;
                apply();
                scene.classList.remove('is-changing');
                schedule();
            }, 850);
        }
        function resetTilt() {
            cancelAnimationFrame(frame);
            scene.style.setProperty('--tilt-x', '0deg');
            scene.style.setProperty('--tilt-y', '0deg');
            scene.style.setProperty('--light-x', '50%');
            scene.style.setProperty('--light-y', '35%');
        }
        function stop() {
            engaged = true;
            schedule();
        }
        root.addEventListener('pointerdown', stop, options);
        root.addEventListener('keydown', stop, options);
        root.addEventListener('focusin', stop, options);
        scene.addEventListener('pointerenter', () => { hovered = true; schedule(); }, options);
        scene.addEventListener('pointerleave', () => { hovered = false; resetTilt(); schedule(); }, options);
        scene.addEventListener('pointermove', (event) => {
            if (event.pointerType !== 'mouse' || !finePointer.matches || reduced.matches) return;
            cancelAnimationFrame(frame);
            frame = requestAnimationFrame(() => {
                const rect = scene.getBoundingClientRect();
                const x = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width));
                const y = Math.max(0, Math.min(1, (event.clientY - rect.top) / rect.height));
                scene.style.setProperty('--tilt-x', `${(0.5 - y) * 8}deg`);
                scene.style.setProperty('--tilt-y', `${(x - 0.5) * 8}deg`);
                scene.style.setProperty('--light-x', `${x * 100}%`);
                scene.style.setProperty('--light-y', `${y * 100}%`);
            });
        }, options);
        document.addEventListener('visibilitychange', () => { resetTilt(); schedule(); }, options);
        reduced.addEventListener('change', () => { resetTilt(); schedule(); }, options);
        const observer = new IntersectionObserver(([entry]) => {
            visible = entry.isIntersecting;
            schedule();
        });
        observer.observe(scene);
        scenes.set(scene, () => {
            disposed = true;
            clearTimeout(timer);
            clearTimeout(transition);
            cancelAnimationFrame(frame);
            observer.disconnect();
            events.abort();
        });
        load(index).then((ready) => {
            if (disposed) return;
            if (ready) apply();
            schedule();
        });
    }

    function scan() {
        for (const [scene, dispose] of scenes) {
            if (!scene.isConnected) { dispose(); scenes.delete(scene); }
        }
        document.querySelectorAll('[data-foil-scene]').forEach(initialize);
    }
    document.addEventListener('htmx:load', scan);
    document.addEventListener('htmx:beforeCleanupElement', (event) => {
        for (const [scene, dispose] of scenes) {
            if (event.detail.elt.contains(scene)) { dispose(); scenes.delete(scene); }
        }
    });
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', scan, { once: true });
    else scan();
})();
