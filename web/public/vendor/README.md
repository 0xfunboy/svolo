# Browser Markdown dependencies

These files are copied unchanged from the packages installed by the exact
versions in `web/package.json` and `web/pnpm-lock.yaml`. They are served locally;
the browser does not load scripts or modules from a CDN.

| Library | Version | Upstream | License |
| --- | --- | --- | --- |
| Marked | 18.1.0 | https://marked.js.org/ and https://registry.npmjs.org/marked | MIT |
| DOMPurify | 3.4.16 | https://github.com/cure53/DOMPurify and https://registry.npmjs.org/dompurify | Apache-2.0 OR MPL-2.0 |

Versions were checked against the official npm registry with `corepack pnpm
view <package> dist-tags --json`; source distributions and licenses come from
those pinned packages. Original license headers, license files and source maps
are retained. When updating a library, update its dependency pin/lock, copy the
matching distribution and license files, and repeat the browser Markdown and
security checks. Marked does not sanitize its output; `markdown.js` applies
DOMPurify as the final transformation with a narrow HTML allowlist.
