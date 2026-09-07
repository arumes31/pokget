// Presentation only: HTMX and the existing server own authentication and routing.
(() => {
    if (document.pokgetAuthUI) return;
    document.pokgetAuthUI = true;

    const authForm = (event) => {
        const form = event.detail.elt?.closest('form');
        return form?.matches('form[data-auth-form]') ? form : null;
    };
    const showError = (event, message) => {
        const form = authForm(event);
        if (!form) return;
        form.setAttribute('aria-busy', 'false');
        const error = form.querySelector('[role="alert"]');
        error.textContent = message;
        error.focus();
    };

    document.addEventListener('htmx:beforeRequest', (event) => {
        const form = authForm(event);
        if (!form) return;
        if (form.getAttribute('aria-busy') === 'true') {
            event.preventDefault();
            return;
        }
        form.querySelector('[role="alert"]').textContent = '';
        form.setAttribute('aria-busy', 'true');
    });
    document.addEventListener('htmx:afterRequest', (event) => {
        authForm(event)?.setAttribute('aria-busy', 'false');
    });
    document.addEventListener('htmx:responseError', (event) => {
        const response = event.detail.xhr;
        // Gateways may return an entire HTML page; never present it as markup.
        const message = response.status >= 500
            ? 'We couldn’t sign you in right now. Please try again shortly.'
            : response.responseText.trim() || 'Something went wrong. Please try again.';
        showError(event, message);
    });
    document.addEventListener('htmx:sendError', (event) => {
        showError(event, 'Couldn’t connect. Check your connection and try again.');
    });
    document.addEventListener('htmx:timeout', (event) => {
        showError(event, 'The request took too long. Please try again.');
    });
})();
