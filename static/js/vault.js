// Pokget Vault Interactive Logic

// BUG-M07 FIX: Guard flag to prevent double initialization when both
// DOMContentLoaded and HTMX afterSwap events fire. Previously, vault.js
// initialized itself on every DOMContentLoaded, but HTMX content swaps
// could trigger a second initialization, causing duplicate event listeners
// and duplicate animations.
let vaultInitialized = false;

function initVault() {
if (vaultInitialized) {
// Re-initialize only rolling numbers for new content (HTMX swaps),
// but skip haptics to avoid duplicate click listeners.
initRollingNumbers();
return;
}
vaultInitialized = true;
initRollingNumbers();
initHaptics();
initHeartbeat();
initPullToRefresh();
initSwipeToDelete();
}

// Use HTMX's afterSettle event when available, fallback to DOMContentLoaded
document.addEventListener('DOMContentLoaded', () => {
initVault();
});

// Re-init rolling numbers after HTMX content swaps (without duplicating haptic listeners)
if (typeof htmx !== 'undefined') {
document.body.addEventListener('htmx:afterSettle', () => {
initRollingNumbers();
});
}

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
		// Skip if already animated (has data-animated attribute)
		if (counter.hasAttribute('data-animated')) {
			return;
		}
		counter.setAttribute('data-animated', 'true');
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
console.log(`[Pokget] Heartbeat successful. XP: ${data.xp}, Rank: ${data.rank}`);
// Optional: Show a subtle notification or update UI
}
} catch (err) {
console.error('[Pokget] Heartbeat failed', err);
}
}, HEARTBEAT_INTERVAL);
}

// MOBILE-11: Pull-to-refresh for card lists
function initPullToRefresh() {
let startY = 0;
let pulling = false;
const PULL_THRESHOLD = 80;
const mainContent = document.getElementById('main-content');
if (!mainContent) return;

mainContent.addEventListener('touchstart', (e) => {
// Only activate when scrolled to top
if (mainContent.scrollTop > 0) return;
startY = e.touches[0].clientY;
pulling = true;
}, { passive: true });

mainContent.addEventListener('touchmove', (e) => {
if (!pulling) return;
const deltaY = e.touches[0].clientY - startY;
if (deltaY > PULL_THRESHOLD && deltaY < PULL_THRESHOLD * 3) {
mainContent.style.setProperty('--ptr-progress', Math.min(deltaY / PULL_THRESHOLD, 1));
if (!mainContent.querySelector('.ptr-indicator')) {
const indicator = document.createElement('div');
indicator.className = 'ptr-indicator';
indicator.innerHTML = '<span class="material-symbols-outlined" style="font-size:20px;animation:spin 1s linear infinite">sync</span>';
indicator.style.cssText = 'text-align:center;padding:8px;color:#ddb7ff;opacity:0.6;transition:transform 0.2s';
mainContent.prepend(indicator);
}
}
}, { passive: true });

mainContent.addEventListener('touchend', () => {
if (!pulling) return;
pulling = false;
const indicator = mainContent.querySelector('.ptr-indicator');
const progress = parseFloat(mainContent.style.getPropertyValue('--ptr-progress') || '0');

if (progress >= 1) {
// Trigger HTMX refresh on the main content
if (typeof htmx !== 'undefined') {
htmx.ajax('GET', '/dashboard', '#main-content');
triggerHaptic([10, 30, 10]);
}
}

if (indicator) {
indicator.remove();
}
mainContent.style.removeProperty('--ptr-progress');
}, { passive: true });
}

// MOBILE-11: Swipe-to-delete for portfolio items
function initSwipeToDelete() {
const cardItems = document.querySelectorAll('.glass-card.group');
cardItems.forEach((item) => {
let startX = 0;
let currentX = 0;
let swiping = false;
const SWIPE_THRESHOLD = 80;

item.addEventListener('touchstart', (e) => {
startX = e.touches[0].clientX;
swiping = true;
item.style.transition = 'none';
}, { passive: true });

item.addEventListener('touchmove', (e) => {
if (!swiping) return;
currentX = e.touches[0].clientX - startX;
if (currentX < -10) {
const translateX = Math.max(currentX, -120);
item.style.transform = `translateX(${translateX}px)`;
}
}, { passive: true });

item.addEventListener('touchend', () => {
if (!swiping) return;
swiping = false;
item.style.transition = 'transform 0.3s ease';

if (currentX < -SWIPE_THRESHOLD) {
// Reveal delete action — snap to left
item.style.transform = 'translateX(-80px)';
// Add delete button if not present
if (!item.querySelector('.swipe-delete-btn')) {
const btn = document.createElement('button');
btn.className = 'swipe-delete-btn';
btn.innerHTML = '<span class="material-symbols-outlined" style="font-size:20px">delete</span>';
btn.style.cssText = 'position:absolute;right:-80px;top:0;bottom:0;width:80px;background:#ef4444;color:white;border:none;display:flex;align-items:center;justify-content:center;cursor:pointer;';
btn.addEventListener('click', () => {
triggerHaptic(20);
// Dispatch HTMX delete or custom event
window.dispatchEvent(new CustomEvent('notify', { detail: { msg: 'Swipe delete triggered', type: 'info' } }));
item.style.transform = 'translateX(0)';
setTimeout(() => btn.remove(), 300);
});
item.style.position = 'relative';
item.appendChild(btn);
}
} else {
item.style.transform = 'translateX(0)';
// Remove any delete button
const btn = item.querySelector('.swipe-delete-btn');
if (btn) btn.remove();
}
currentX = 0;
}, { passive: true });
});
}
