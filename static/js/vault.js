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
	syncActiveView();
}

// Use HTMX's afterSettle event when available, fallback to DOMContentLoaded
document.addEventListener('DOMContentLoaded', () => {
	initVault();
});

// Re-init rolling numbers after HTMX content swaps (without duplicating haptic listeners)
if (typeof htmx !== 'undefined') {
	document.body.addEventListener('htmx:afterSettle', (event) => {
		initRollingNumbers();
		initSwipeToDelete(event.detail.target || document);
	});
	document.body.addEventListener('htmx:historyRestore', syncActiveView);
}

window.addEventListener('popstate', () => requestAnimationFrame(syncActiveView));

function currentView() {
	const view = new URLSearchParams(window.location.search).get('view');
	if (view) return view;

	const pathViews = {
		'/dashboard': 'home',
		'/wantlist': 'wantlist',
		'/binders': 'binders',
		'/centering': 'scan',
		'/errors': 'errors',
		'/trade': 'trade',
		'/settings': 'settings'
	};
	return pathViews[window.location.pathname] || 'home';
}

function syncActiveView() {
	window.dispatchEvent(new CustomEvent('pokget-view-change', {
		detail: { view: currentView() }
	}));
}

function currentFragmentPath() {
	if (window.location.pathname !== '/') return window.location.pathname;

	const params = new URLSearchParams(window.location.search);
	const view = params.get('view') || 'home';
	if (view === 'binders' && params.get('binder')) {
		return '/binders/' + encodeURIComponent(params.get('binder'));
	}

	const routes = {
		home: '/dashboard',
		wantlist: '/wantlist',
		binders: '/binders',
		scan: '/centering',
		errors: '/errors',
		trade: '/trade',
		settings: '/settings'
	};
	return routes[view] || routes.home;
}

// Improvement #8: Rolling Number Animation
function animateValue(obj, start, end, duration, prefix = '', suffix = '', decimals = 0) {
	let startTimestamp = null;
	const step = (timestamp) => {
		if (!startTimestamp) startTimestamp = timestamp;
		const progress = Math.min((timestamp - startTimestamp) / duration, 1);
		const value = decimals === 0
			? Math.floor(progress * (end - start) + start)
			: progress * (end - start) + start;
		if (decimals === 0) {
			obj.textContent = prefix + value.toLocaleString() + suffix;
		} else {
			obj.textContent = prefix + value.toLocaleString(undefined, {
				minimumFractionDigits: decimals,
				maximumFractionDigits: decimals
			}) + suffix;
		}
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
		const decimals = Number.parseInt(counter.getAttribute('data-decimals') || '0', 10);
		animateValue(counter, 0, target, 900, prefix, suffix, decimals);
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
				htmx.ajax('GET', currentFragmentPath(), { target: '#main-content', source: document.body });
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
function initSwipeToDelete(root = document) {
	const cardItems = root.querySelectorAll('[data-portfolio-item]:not([data-swipe-ready])');
	cardItems.forEach((item) => {
		item.dataset.swipeReady = 'true';
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
					btn.setAttribute('aria-label', 'Remove card from vault');
					btn.addEventListener('click', async () => {
						triggerHaptic(20);
						const itemId = item.getAttribute('data-id');
						if (itemId && typeof htmx !== 'undefined') {
							const csrfToken = document.querySelector('meta[name="csrf-token"]')?.content || '';
							let response;
							try {
								response = await fetch('/portfolio/delete', {
									method: 'POST',
									headers: {
										'Content-Type': 'application/x-www-form-urlencoded',
										'X-CSRF-Token': csrfToken
									},
									body: new URLSearchParams({ item_id: itemId })
								});
							} catch {
								window.dispatchEvent(new CustomEvent('notify', { detail: { msg: 'Network error while removing card', type: 'error' } }));
								item.style.transform = 'translateX(0)';
								btn.remove();
								return;
							}
							if (!response.ok) {
								const message = (await response.text()).trim() || 'Unable to remove card';
								window.dispatchEvent(new CustomEvent('notify', { detail: { msg: message, type: 'error' } }));
								item.style.transform = 'translateX(0)';
								btn.remove();
								return;
							}
							item.style.transform = 'translateX(-120px)';
							item.style.opacity = '0';
							item.style.transition = 'transform 0.3s ease, opacity 0.3s ease';
							window.dispatchEvent(new CustomEvent('notify', { detail: { msg: 'Card removed from vault', type: 'success' } }));
							setTimeout(() => {
								item.remove();
								htmx.ajax('GET', '/dashboard', { target: '#main-content', source: document.body });
							}, 300);
						} else {
							window.dispatchEvent(new CustomEvent('notify', { detail: { msg: 'Swipe delete triggered', type: 'info' } }));
							item.style.transform = 'translateX(0)';
							setTimeout(() => btn.remove(), 300);
						}
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
