(() => {
  'use strict';

  const storageKey = 'pokget-theme';
  const system = window.matchMedia('(prefers-color-scheme: dark)');
  const normalize = value => ['light', 'dark', 'system'].includes(value) ? value : 'system';
  let preference = 'system';
  try {
    preference = normalize(window.localStorage.getItem(storageKey));
  } catch (_) {
    // Storage can be unavailable; theme selection still works for this page.
  }

  function syncControls() {
    document.querySelectorAll('[data-theme-select]').forEach(select => {
      select.value = preference;
    });
  }

  function apply() {
    const theme = preference === 'system' ? (system.matches ? 'dark' : 'light') : preference;
    document.documentElement.dataset.theme = theme;
    document.documentElement.classList.toggle('dark', theme === 'dark');
    document.documentElement.style.colorScheme = theme;
    syncControls();
  }

  // This script runs in the head, before styles and the first paint.
  apply();
  system.addEventListener('change', apply);
  document.addEventListener('DOMContentLoaded', syncControls);
  document.addEventListener('htmx:afterSwap', syncControls);
  document.addEventListener('change', event => {
    if (!event.target.matches('[data-theme-select]')) return;
    preference = normalize(event.target.value);
    try {
      window.localStorage.setItem(storageKey, preference);
    } catch (_) {
      // Keep the selected theme even if the browser cannot persist it.
    }
    apply();
  });
  window.addEventListener('storage', event => {
    if (event.key !== storageKey && event.key !== null) return;
    preference = normalize(event.newValue);
    apply();
  });
})();
