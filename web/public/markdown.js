import { t, locale } from './i18n.js?v=20261006-11';
import { Marked, Renderer } from './vendor/marked-18.1.0/marked.esm.js?v=20261006-11';
import createDOMPurify from './vendor/dompurify-3.4.16/purify.es.mjs?v=20261006-11';

const escapeHTML = value => String(value).replace(/[&<>"']/g, character => ({
  '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;',
})[character]);

function safeHref(value) {
  const encoded = String(value ?? '').trim();
  if (!encoded || /[\u0000-\u001f\u007f-\u009f]/.test(encoded)) return null;
  try {
    // GFM destinations may contain HTML entities. Escape angle brackets before
    // decoding in a text-only fragment, then check the decoded protocol as well.
    const decoder = document.createElement('span');
    decoder.innerHTML = encoded.replace(/</g, '&lt;').replace(/>/g, '&gt;');
    const href = decoder.textContent.trim();
    if (!href || /[\u0000-\u001f\u007f-\u009f]/.test(href)) return null;
    const url = new URL(href, window.location.href);
    return ['https:', 'http:', 'mailto:'].includes(url.protocol) ? url.href : null;
  } catch {
    return null;
  }
}

const renderer = new Renderer();
// Model output is Markdown, never a source of executable HTML.
renderer.html = ({ text }) => escapeHTML(text);
renderer.link = function ({ href, title, tokens }) {
  const label = this.parser.parseInline(tokens);
  const destination = safeHref(href);
  if (!destination) return label;
  const tooltip = title ? ` title="${escapeHTML(title)}"` : '';
  return `<a href="${escapeHTML(destination)}"${tooltip} target="_blank" rel="noopener noreferrer">${label}</a>`;
};
// Images become explicit links: rendering a response never fetches remote media.
renderer.image = ({ href, title, text }) => {
  const label = `${t("Immagine:")} ${escapeHTML(text || title || t("Apri immagine"))}`;
  const destination = safeHref(href);
  return destination
    ? `<a class="markdown-image-link" href="${escapeHTML(destination)}" target="_blank" rel="noopener noreferrer">${label}</a>`
    : `<span class="markdown-image-link">${label}</span>`;
};
renderer.table = function (token) {
  return `<div class="markdown-table-scroll" role="region" aria-label="${t("Tabella")}" tabindex="0">${Renderer.prototype.table.call(this, token)}</div>`;
};
renderer.checkbox = ({ checked }) => `<span class="markdown-task-check" role="checkbox" aria-checked="${checked ? 'true' : 'false'}" aria-disabled="true">${checked ? '☑' : '☐'}</span> `;

const markdown = new Marked({ renderer, gfm:true, breaks:false, async:false });
const purifier = typeof window === 'undefined' ? null : createDOMPurify(window);
const sanitizeOptions = {
  ALLOWED_TAGS:['p','br','strong','em','del','blockquote','h1','h2','h3','h4','h5','h6',
    'ul','ol','li','pre','code','hr','table','thead','tbody','tr','th','td','a','div','span'],
  ALLOWED_ATTR:['href','title','target','rel','class','align','start','role','tabindex',
    'aria-label','aria-checked','aria-disabled'],
  ALLOW_DATA_ATTR:false,
  ALLOW_ARIA_ATTR:false,
  ALLOW_UNKNOWN_PROTOCOLS:false,
  RETURN_TRUSTED_TYPE:false,
};

/** Return sanitized GFM HTML synchronously, including incomplete streaming text. */
export function renderMarkdown(text) {
  const source = String(text ?? '');
  if (!source) return '';
  const fallback = () => `<p class="markdown-fallback">${escapeHTML(source)}</p>`;
  if (!purifier?.isSupported) return fallback();
  try {
    // Sanitization is the final transformation; do not interpolate untrusted HTML
    // into the returned result or post-process it with an HTML-generating library.
    return purifier.sanitize(markdown.parse(source), sanitizeOptions);
  } catch {
    return fallback();
  }
}
