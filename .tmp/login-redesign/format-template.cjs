const fs = require('node:fs');
const file = 'templates/auth_fragment.html';
const text = fs.readFileSync(file, 'utf8').replace(/^ (.*)$/gm, (_, line) => line.trim() ? ((line.startsWith('</div>') || line.startsWith('</button>')) ? '            ' : '                ') + line : '').replace(/\n\n\n/g, '\n\n');
fs.writeFileSync(file, text);
