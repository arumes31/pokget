// Pokget Vault Interactive Logic

document.addEventListener('DOMContentLoaded', () => {
    initRollingNumbers();
    initHaptics();
});

// Improvement #8: Rolling Number Animation
function animateValue(obj, start, end, duration, prefix = '', suffix = '') {
    let startTimestamp = null;
    const step = (timestamp) => {
        if (!startTimestamp) startTimestamp = timestamp;
        const progress = Math.min((timestamp - startTimestamp) / duration, 1);
        const value = Math.floor(progress * (end - start) + start);
        obj.innerHTML = prefix + value.toLocaleString() + suffix;
        if (progress < 1) {
            window.requestAnimationFrame(step);
        }
    };
    window.requestAnimationFrame(step);
}

function initRollingNumbers() {
    const counters = document.querySelectorAll('.roll-counter');
    counters.forEach(counter => {
        const target = parseFloat(counter.getAttribute('data-target'));
        const prefix = counter.getAttribute('data-prefix') || '';
        const suffix = counter.getAttribute('data-suffix') || '';
        animateValue(counter, 0, target, 2000, prefix, suffix);
    });
}

// Improvement #10: Haptic Feedback
function triggerHaptic(pattern = 10) {
    if ('vibrate' in navigator) {
        navigator.vibrate(pattern);
    }
}

function initHaptics() {
    document.querySelectorAll('button, .glass-button, .scan-fab').forEach(el => {
        el.addEventListener('click', () => triggerHaptic(15));
    });
}

// Improvement #11: Optimistic UI for Portfolio Addition
document.body.addEventListener('htmx:beforeRequest', (evt) => {
    const target = evt.detail.target;
    if (target.classList.contains('add-card-btn')) {
        target.innerHTML = '<span class="material-symbols-outlined animate-spin">sync</span>';
        target.disabled = true;
        triggerHaptic([10, 30, 10]);
    }
});

// Passive XP Heartbeat (1 XP per 15 minutes)
function initHeartbeat() {
    const HEARTBEAT_INTERVAL = 15 * 60 * 1000; // 15 minutes
    
    setInterval(async () => {
        const csrfToken = document.querySelector('meta[name="csrf-token"]')?.getAttribute('content');
        if (!csrfToken) return;

        try {
            const response = await fetch('/api/gamification/heartbeat', {
                method: 'POST',
                headers: {
                    'X-CSRF-Token': csrfToken,
                    'Content-Type': 'application/json'
                }
            });
            
            if (response.ok) {
                const data = await response.json();
                // Optional: Show a subtle notification or update UI
            }
        } catch (err) {
            console.error('[Pokget] Heartbeat failed', err);
        }
    }, HEARTBEAT_INTERVAL);
}

document.addEventListener('DOMContentLoaded', () => {
    initRollingNumbers();
    initHaptics();
    initHeartbeat();
});
