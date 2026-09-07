const fs = require('node:fs');
const file = 'templates/auth_fragment.html';
const old = fs.readFileSync(file, 'utf8');
let tabs = old.slice(old.indexOf('        <!-- Mode tabs -->'), old.indexOf('        <!-- Login Form -->'));
tabs = tabs.replace('grid grid-cols-2 gap-1 p-1 mb-6 bg-zinc-100 rounded-xl', 'auth-tabs')
 .replaceAll('min-h-11 rounded-lg text-sm font-semibold transition-colors', 'auth-tab')
 .replaceAll("'bg-white text-zinc-900 shadow-sm' : 'text-zinc-500 hover:text-zinc-700'", "'is-active' : ''");
let resend = old.slice(old.indexOf('        <!-- Resend verification -->'), old.indexOf('    <p class="text-center text-xs'));
// This slice includes the old panel closing div; retain it for the new panel.
resend = resend.replace('text-sm text-zinc-500 mb-1', 'auth-muted').replace('btn-ghost text-violet-700', 'btn-ghost auth-text-action');
let success = old.slice(old.indexOf('{{define "auth_success"}}'));
success = success.replace('w-full max-w-md mx-auto', 'auth-success w-full max-w-md mx-auto');
const eye = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/><path x-show="show" d="m3 3 18 18"/></svg>`;
function field(id, label, name, type, autocomplete, enter) {
 const password = type === 'password';
 return `<div class="auth-field"${password ? ' x-data="{ show: false }"' : ''}>
 <input id="${id}" class="input" placeholder=" " type="${type}" ${password ? ':type="show ? \'text\' : \'password\'"' : 'inputmode="email" autocapitalize="none" spellcheck="false"'} autocomplete="${autocomplete}" enterkeyhint="${enter}" name="${name}" required />
 <label for="${id}">${label}</label>
 ${password ? `<button type="button" class="auth-password-toggle" @click="show = !show" :aria-label="show ? 'Hide password' : 'Show password'" :aria-pressed="show.toString()" aria-controls="${id}" aria-label="Show password">${eye}</button>` : ''}
 </div>`;
}
function button(text, pending, register = false) {
 return `<button type="submit" class="btn-primary auth-submit"${register ? ' @click="signupAttempted = true"' : ''}>
 <span class="auth-submit-idle">${text}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="M5 12h14m-5-5 5 5-5 5"/></svg></span>
 <span class="auth-submit-pending" role="status"><span class="auth-spinner" aria-hidden="true"></span>${pending}</span>
 </button>`;
}
const content = `<link rel="stylesheet" href="/static/css/auth.css?v={{ .BuildVersion }}">
<script src="/static/js/auth.js?v={{ .BuildVersion }}" defer></script>
<div class="auth-experience" x-data="{ mode: 'login', signupAttempted: false, resendDisabled: false, countdown: 0 }">
    <section class="auth-showcase" aria-label="Your collection starts here">
        <div class="auth-showcase-label" aria-hidden="true"><span></span> A LITTLE WONDER. EVERY CARD.</div>
        <div class="auth-card-scene" aria-hidden="true">
            <div class="auth-orbit"></div>
            <span class="auth-dust auth-dust-one"></span><span class="auth-dust auth-dust-two"></span><span class="auth-dust auth-dust-three"></span>
            <div class="auth-collectible auth-card-left">
                <div class="auth-card-top"><span>NO. 018</span><span>◆ RARE</span></div>
                <svg class="auth-card-art" viewBox="0 0 200 270" fill="none"><defs><linearGradient id="auth-tide" x2="0" y2="1"><stop stop-color="#182e43"/><stop offset="1" stop-color="#438f9d"/></linearGradient></defs><path fill="url(#auth-tide)" d="M0 0h200v270H0z"/><circle cx="100" cy="90" r="48" stroke="#91d2ca" stroke-opacity=".6"/><circle cx="100" cy="90" r="36" stroke="#91d2ca" stroke-opacity=".25"/><path d="m100 33 28 57-28 57-28-57Z" fill="#9ddbcf" fill-opacity=".65"/><path d="m100 33 4 57-4 57-28-57Z" fill="#d7f8e7" fill-opacity=".6"/><path d="M-20 185q60-65 120 0t120 0M-20 207q60-65 120 0t120 0M-20 229q60-65 120 0t120 0M-20 251q60-65 120 0t120 0" stroke="#b2efdf" stroke-opacity=".3"/></svg>
                <div class="auth-card-caption"><span>Tidal prism</span><small>THE ELEMENTAL SERIES</small></div>
            </div>
            <div class="auth-collectible auth-card-right">
                <div class="auth-card-top"><span>NO. 032</span><span>◆◆ EPIC</span></div>
                <svg class="auth-card-art" viewBox="0 0 200 270" fill="none"><defs><linearGradient id="auth-solar" x2="0" y2="1"><stop stop-color="#3a2843"/><stop offset="1" stop-color="#b17e65"/></linearGradient></defs><path fill="url(#auth-solar)" d="M0 0h200v270H0z"/><circle cx="100" cy="100" r="49" fill="#efd1ac"/><circle cx="117" cy="82" r="40" fill="#705063"/><circle cx="100" cy="100" r="68" stroke="#efd1ac" stroke-opacity=".4"/><path d="m0 222 43-73 35 47 42-58 80 95v37H0Z" fill="#392c42"/><path d="m0 247 58-39 27 20 47-31 68 42v31H0Z" fill="#221f33"/><path d="M32 36h8m-4-4v8m116 5h8m-4-4v8" stroke="#efd1ac"/></svg>
                <div class="auth-card-caption"><span>Solstice</span><small>THE CELESTIAL SERIES</small></div>
            </div>
            <div class="auth-collectible auth-card-hero">
                <div class="auth-card-top"><span>NO. 001</span><span>◆◆◆ LEGENDARY</span></div>
                <svg class="auth-card-art" viewBox="0 0 200 270" fill="none"><defs><linearGradient id="auth-aurora" x2=".8" y2="1"><stop stop-color="#212240"/><stop offset=".5" stop-color="#796493"/><stop offset="1" stop-color="#7db4bc"/></linearGradient><linearGradient id="auth-crystal" x2="1" y2="1"><stop stop-color="#f2e3ff"/><stop offset="1" stop-color="#a8e8dd"/></linearGradient></defs><path fill="url(#auth-aurora)" d="M0 0h200v270H0z"/><circle cx="100" cy="116" r="73" stroke="#e1d9f1" stroke-opacity=".3"/><circle cx="100" cy="116" r="58" stroke="#e1d9f1" stroke-opacity=".22"/><path d="m100 42 42 68-42 82-42-82Z" fill="url(#auth-crystal)"/><path d="m100 42 5 70-5 80-42-82Z" fill="#fff" fill-opacity=".3"/><path d="m100 42 42 68-37 2Z" fill="#fff" fill-opacity=".35"/><path d="m58 110 47 2-5 80Z" fill="#a99cca" fill-opacity=".75"/><ellipse cx="100" cy="219" rx="38" ry="7" fill="#24283f" fill-opacity=".35"/><path d="M28 55h10m-5-5v10m126 16h8m-4-4v8M43 179h6m-3-3v6" stroke="#fff" stroke-opacity=".7"/><circle cx="140" cy="34" r="1.5" fill="#fff"/><circle cx="30" cy="125" r="1" fill="#fff"/></svg>
                <div class="auth-card-caption"><span>Aurora shard <i>✧</i></span><small>THE ORIGIN SERIES <b>001 / 100</b></small></div>
            </div>
        </div>
        <div class="auth-showcase-copy"><p class="auth-eyebrow">FOR THE COLLECTOR IN YOU</p><h2>Every card.<br>A new discovery.</h2><p>The thrill of the find. The joy of a complete set.<br>A home for the cards that mean something to you.</p><div class="auth-series-note"><span>✧</span> Collect. Discover. Complete.</div></div>
    </section>
    <section class="auth-panel" aria-labelledby="auth-title">
        <header class="auth-brand">
            <div class="auth-wordmark"><img src="/static/img/logo-128.webp" alt="" width="56" height="56"><span>Pokget</span></div>
            <p>Your collection, in your pocket.</p>
        </header>
        <div class="auth-heading"><p class="auth-eyebrow" x-text="mode === 'login' ? 'YOUR NEXT DISCOVERY AWAITS' : 'MAKE ROOM FOR SOMETHING RARE'">YOUR NEXT DISCOVERY AWAITS</p><h1 id="auth-title" x-text="mode === 'login' ? 'Welcome back.' : 'Start your collection.'">Welcome back.</h1><p x-text="mode === 'login' ? 'Sign in. Pick up where you left off.' : 'A home for every card you collect.'">Sign in. Pick up where you left off.</p></div>
${tabs}
        <form id="login-panel" role="tabpanel" aria-labelledby="login-tab" x-show="mode === 'login'" hx-post="/auth/login" hx-target="#main-content" hx-sync="this:drop" hx-disabled-elt="find button[type='submit']" hx-request='{"timeout":30000}' data-auth-form aria-busy="false" class="auth-form">
            <input type="hidden" name="gorilla.csrf.Token" value="{{.CSRFToken}}">
            <div id="auth-error" role="alert" aria-live="assertive" tabindex="-1" class="auth-error"></div>
            ${field('login-email','Email address','email','email','username','next')}
            ${field('login-password','Password','password','password','current-password','send')}
            <label class="auth-remember"><input type="checkbox" name="remember"><span>Remember me</span></label>
            ${button('Sign in','Signing in…')}
            <p class="auth-switch">New collector? <button type="button" @click="mode = 'register'; $nextTick(() => document.getElementById('register-email').focus())">Create account</button></p>
        </form>
        <form id="register-panel" role="tabpanel" aria-labelledby="register-tab" x-show="mode === 'register'" x-cloak hx-post="/auth/register" hx-target="#main-content" hx-sync="this:drop" hx-disabled-elt="find button[type='submit']" hx-request='{"timeout":30000}' data-auth-form aria-busy="false" class="auth-form">
            <input type="hidden" name="gorilla.csrf.Token" value="{{.CSRFToken}}">
            <div id="auth-register-error" role="alert" aria-live="assertive" tabindex="-1" class="auth-error"></div>
            ${field('register-email','Email address','email','email','email','next')}
            ${field('register-password','Password','password','password','new-password','next')}
            ${field('register-confirm-password','Confirm password','confirm_password','password','new-password','send')}
            ${button('Create account','Creating account…',true)}
            <p class="auth-switch">Already a collector? <button type="button" @click="mode = 'login'; signupAttempted = false; $nextTick(() => document.getElementById('login-email').focus())">Sign in</button></p>
        </form>
${resend.replace(/    <\/div>\s*$/, '    </section>')}
    <footer class="auth-footer"><span>© 2026 Pokget</span><span>Made for the love of collecting.</span></footer>
</div>
${success}`;
fs.writeFileSync('.tmp/login-redesign/auth_fragment.html', content);

