import assert from 'node:assert/strict';
import test from 'node:test';
import { nativeBrowser, browserAvailable } from './test-browser.mjs';
test('actual Markdown renderer in a sandboxed Chromium browser without CSP', { skip:browserAvailable ? false : 'Native browser unavailable; set SVOLO_CHROMIUM or SVOLO_TEST_ELECTRON (with Xvfb) to run Markdown security checks' }, async t => {
  const { evaluate, requests, origin } = await nativeBrowser(t);
  const inspect = async source => evaluate(`(async()=>{globalThis.__markdownXSS=0;const output=document.getElementById('output');output.innerHTML=renderMarkdown(${JSON.stringify(source)});await new Promise(resolve=>setTimeout(resolve,30));return {html:output.innerHTML,text:output.textContent,xss:globalThis.__markdownXSS,dangerous:output.querySelectorAll('script,img,svg,math,iframe,object,embed,style,form,input,button').length,eventAttributes:[...output.querySelectorAll('*')].flatMap(element=>[...element.attributes].filter(attribute=>attribute.name.toLowerCase().startsWith('on')).map(attribute=>attribute.name)),links:[...output.querySelectorAll('a')].map(link=>({text:link.textContent,href:link.href,protocol:link.protocol,target:link.target,rel:link.rel}))};})()`);

  await t.test('raw HTML and mutation payloads stay inert in the real DOM', async () => {
    const source = '<img src="/unexpected-image.png" onerror="globalThis.__markdownXSS=1">\n<script>globalThis.__markdownXSS=2</script>\n<svg onload="globalThis.__markdownXSS=3"><a href="javascript:globalThis.__markdownXSS=4">x</a></svg>\n<math><mtext><table><mglyph><style><!--</style><img src="/unexpected-image.png" onerror="globalThis.__markdownXSS=5">\n<form id="app"><input name="renderMarkdown"></form>';
    const result = await inspect(source);
    assert.equal(result.xss, 0); assert.equal(result.dangerous, 0); assert.deepEqual(result.eventAttributes, []);
    assert.match(result.text, /<img/);
    assert.equal(requests.filter(path => path === '/unexpected-image.png').length, 0);
  });

  await t.test('script, data and control-character link destinations cannot execute', async () => {
    const source = '[script](javascript:globalThis.__markdownXSS=1)\n[mixed](JaVaScRiPt:globalThis.__markdownXSS=2)\n[entity](javascript&#58;globalThis.__markdownXSS=3)\n[control](java&#x09;script:globalThis.__markdownXSS=4)\n[data](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)\n[vb](vbscript:msgbox)';
    const result = await inspect(source);
    assert.equal(result.xss, 0); assert.equal(result.dangerous, 0); assert.deepEqual(result.eventAttributes, []);
    for (const link of result.links) assert.ok(['http:','https:','mailto:'].includes(link.protocol), `unsafe protocol ${link.protocol}`);
    const clicked = await evaluate(`(()=>{const output=document.getElementById('output');for(const link of output.querySelectorAll('a'))if(!['http:','https:','mailto:'].includes(link.protocol))link.click();return globalThis.__markdownXSS;})()`);
    assert.equal(clicked, 0);
  });

  await t.test('safe links are explicit and protect opener and referrer', async () => {
    const result = await inspect('[HTTPS](https://example.test/path) [HTTP](http://example.test/) [Mail](mailto:approved@example.test) [Relative](/relative/path)');
    assert.equal(result.links.length, 4);
    assert.equal(result.links[3].href, origin + '/relative/path');
    for (const link of result.links) { assert.equal(link.target, '_blank'); assert.ok(link.rel.split(' ').includes('noopener')); assert.ok(link.rel.split(' ').includes('noreferrer')); }
  });

  await t.test('HTML entities preserve query parameters and cannot hide executable schemes', async () => {
    const result = await inspect('[query](https://example.test/?a=1&amp;b=2) [numeric](https://example.test/?a=1&#38;b=2) [encoded-scheme](javascript&#x3a;globalThis.__markdownXSS=12) [encoded-control](java&#x09;script:globalThis.__markdownXSS=13)');
    assert.equal(result.links.length, 2);
    for (const link of result.links) assert.equal(link.href, 'https://example.test/?a=1&b=2');
    assert.equal(result.dangerous, 0); assert.deepEqual(result.eventAttributes, []); assert.equal(result.xss, 0);
  });

  await t.test('Markdown images never fetch media during rendering', async () => {
    const result = await inspect('![private media](/unexpected-markdown-image.png)\n![invalid](data:image/svg+xml;base64,PHN2Zy8+)');
    assert.equal(result.dangerous, 0); assert.equal(result.links.length, 1); assert.match(result.text, /Image: private media/);
    assert.equal(requests.filter(path => path === '/unexpected-markdown-image.png').length, 0);
  });

  await t.test('GFM tables, task lists, headings and inline formatting produce semantic DOM', async () => {
    const source = '# Heading\n\n**Strong** *emphasis* ~~removed~~ `inline`\n\n| Name | State |\n| --- | ---: |\n| Alpha | Ready |\n| Beta | Waiting |\n\n- [x] Verified\n- [ ] Pending\n\n> Quoted text';
    await inspect(source);
    const result = await evaluate(`(()=>{const output=document.getElementById('output');return {heading:output.querySelector('h1')?.textContent,strong:output.querySelector('strong')?.textContent,em:output.querySelector('em')?.textContent,del:output.querySelector('del')?.textContent,inline:output.querySelector('code')?.textContent,rows:output.querySelectorAll('tbody tr').length,headers:output.querySelectorAll('th').length,wrapper:output.querySelector('table')?.parentElement.className,checks:[...output.querySelectorAll('[role="checkbox"]')].map(check=>({checked:check.getAttribute('aria-checked'),disabled:check.getAttribute('aria-disabled')})),quote:output.querySelector('blockquote')?.textContent};})()`);
    assert.equal(result.heading, 'Heading'); assert.equal(result.strong, 'Strong'); assert.equal(result.em, 'emphasis'); assert.equal(result.del, 'removed'); assert.equal(result.inline, 'inline');
    assert.equal(result.rows, 2); assert.equal(result.headers, 2); assert.equal(result.wrapper, 'markdown-table-scroll');
    assert.deepEqual(result.checks, [{ checked:'true',disabled:'true' }, { checked:'false',disabled:'true' }]);
    assert.match(result.quote, /Quoted text/);
  });

  await t.test('fenced code preserves hostile source as readable text', async () => {
    const hostile = '<img src="/unexpected-code-image.png" onerror="globalThis.__markdownXSS=9">\n<script>globalThis.__markdownXSS=10</script>';
    const result = await inspect('```html\n' + hostile + '\n```');
    assert.equal(result.dangerous, 0); assert.equal(result.xss, 0);
    const code = await evaluate('document.querySelector("#output pre code").textContent');
    assert.equal(code.trimEnd(), hostile);
    assert.equal(requests.filter(path => path === '/unexpected-code-image.png').length, 0);
  });

  await t.test('every incomplete streaming prefix remains synchronous and safe', async () => {
    const source = '**Strong** [unsafe](javascript:globalThis.__markdownXSS=8)\n\n```html\n<img src="/unexpected-stream-image.png" onerror="globalThis.__markdownXSS=9">\n```\n\n| A | B |\n| - | - |\n| one | two |';
    const result = await evaluate(`(()=>{globalThis.__markdownXSS=0;const output=document.getElementById('output');const source=${JSON.stringify(source)};for(let i=0;i<=source.length;i++){const html=renderMarkdown(source.slice(0,i));if(typeof html!=='string')throw new Error('renderer became asynchronous');output.innerHTML=html;if(output.querySelector('script,img,svg,iframe,style,form'))throw new Error('unsafe streaming prefix '+i);for(const link of output.querySelectorAll('a'))if(!['http:','https:','mailto:'].includes(link.protocol))throw new Error('unsafe streaming link '+i);}return {prefixes:source.length+1,xss:globalThis.__markdownXSS};})()`);
    assert.equal(result.prefixes, source.length + 1); assert.equal(result.xss, 0);
    assert.equal(requests.filter(path => path === '/unexpected-stream-image.png').length, 0);
  });
});
