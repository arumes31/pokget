const fs = require('node:fs');
let html = fs.readFileSync('templates/auth_fragment.html', 'utf8');
// Normalize the indentation inherited from the first design pass.
html = html.split('\n').map(line => line.startsWith('               ') ? line.slice(15) : line).join('\n');
const cards = [
 ['left',0,'018','RARE','Tidal prism'],
 ['right',2,'032','EPIC','Solstice'],
 ['hero',1,'001','LEGENDARY','Aurora shard'],
].map(([side,index,number,rarity,name])=>`            <div class="auth-collectible auth-card-${side}">
                <div class="auth-card-face">
                    <div class="auth-card-art" data-art-position="${index}"></div>
                    <div class="auth-card-top"><span>NO. ${number}</span><span>${rarity}</span></div>
                    <div class="auth-card-caption">
                        <span data-card-name="${index}">${name}</span>
                        <small>POKGET ORIGINALS <svg viewBox="0 0 20 12" fill="none" stroke="currentColor" stroke-width=".8"><path d="m10 1 5 5-5 5-5-5Z"/><path d="m4 3-3 3 3 3m12-6 3 3-3 3"/></svg></small>
                    </div>
                    <div class="auth-card-reflection"></div>
                </div>
            </div>`).join('\n');
const start = html.indexOf('    <section class="auth-showcase"');
const end = html.indexOf('    <section class="auth-panel"');
if (start<0||end<0) throw new Error('Showcase boundary not found');
const showcase = `    <section class="auth-showcase" aria-label="Your collection starts here">
        <div class="auth-card-scene" data-foil-scene aria-hidden="true">
            <div class="auth-orbit"></div>
            <span class="auth-dust auth-dust-one"></span><span class="auth-dust auth-dust-two"></span>
${cards}
        </div>
        <div class="auth-showcase-copy">
            <h2>Every card.<br>A new discovery.</h2>
            <p>The thrill of the find. The joy of a complete set.<br>A home for the cards that mean something to you.</p>
            <div class="auth-series-note"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" aria-hidden="true"><path d="m12 3 3 6 6 3-6 3-3 6-3-6-6-3 6-3Z"/></svg>Collect. Discover. Complete.</div>
        </div>
    </section>
`;
html=html.slice(0,start)+showcase+html.slice(end);
html=html.replace('<script src="/static/js/auth.js?v={{ .BuildVersion }}" defer></script>', '<script src="/static/js/auth.js?v={{ .BuildVersion }}" defer></script>\n<script src="/static/js/auth-art.js?v={{ .BuildVersion }}" defer></script>');
html=html.replace(/<p class="auth-eyebrow" x-text="[^"]*">[^<]*<\/p>/,'');
fs.writeFileSync('.tmp/login-overdrive/auth_fragment.html',html);
