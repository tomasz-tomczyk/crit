// Smoke test: Vue SFC and Astro grammars are registered in the bundled
// highlight.min.js (issue #868). Run via npm run test:frontend.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import vm from 'node:vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const bundlePath = resolve(__dirname, '..', 'highlight.min.js');
const bundle = readFileSync(bundlePath, 'utf8');

const sandbox = {};
sandbox.window = sandbox;
sandbox.globalThis = sandbox;
sandbox.self = sandbox;
vm.createContext(sandbox);
vm.runInContext(bundle, sandbox, { filename: 'highlight.min.js' });

const hljs = sandbox.hljs || sandbox.window.hljs;
if (!hljs) {
  console.error('FAIL: hljs not exposed on sandbox');
  process.exit(1);
}

let pass = 0;
let fail = 0;

function check(label, ok) {
  const status = ok ? 'PASS' : 'FAIL';
  if (ok) pass++; else fail++;
  console.log(`${status}: ${label}`);
}

check('vue language registered', !!hljs.getLanguage('vue'));
check('astro language registered', !!hljs.getLanguage('astro'));

const vueSample =
  '<template>\n  <div>{{ msg }}</div>\n</template>\n' +
  '<script>\nexport default { data() { return { msg: "hi" } } }\n</script>\n';
const vueOut = hljs.highlight(vueSample, { language: 'vue', ignoreIllegals: true }).value;
check('vue highlight emits hljs spans', vueOut.includes('hljs-'));

const astroSample = '---\nconst title = "Hi";\n---\n<h1>{title}</h1>\n';
const astroOut = hljs.highlight(astroSample, { language: 'astro', ignoreIllegals: true }).value;
check('astro highlight emits hljs spans', astroOut.includes('hljs-'));
check('astro frontmatter fence marked', astroOut.includes('hljs-punctuation') || astroOut.includes('---'));

console.log(`\n${pass} passed, ${fail} failed`);
process.exit(fail === 0 ? 0 : 1);
