'use strict';

// Shared by the app shell and standalone pages that render a modal.
document.addEventListener('keydown', (event) => {
    if (event.key !== 'Tab') return;
    const dialogs = [...document.querySelectorAll('[role="dialog"][aria-modal="true"]')]
        .filter((dialog) => dialog.getClientRects().length > 0);
    const dialog = dialogs.at(-1);
    if (!dialog) return;
    const controls = [...dialog.querySelectorAll('a[href], button, input, select, textarea, [tabindex]')]
        .filter((element) => !element.disabled && element.tabIndex >= 0 && element.getClientRects().length > 0 && !element.closest('[inert]'));
    const first = controls[0];
    const last = controls.at(-1);
    if (!first) {
        event.preventDefault();
        return;
    }
    if (!dialog.contains(document.activeElement) || (event.shiftKey && document.activeElement === first)) {
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
    } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
    }
});
