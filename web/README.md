# Vrac web interface

The optional Vrac frontend is a React/TypeScript outliner built with Vite,
Tailwind CSS, and shadcn/ui components backed by Radix primitives. The component
sources in `src/components/ui` were generated with the official shadcn CLI and
are kept in the repository so they can be maintained with the application.

```sh
npm ci
npm run check
npm test
npm run build
```

The production files are written to `dist/` and embedded into the Go service.
For development, start the service on `127.0.0.1:8080`, then run `npm run dev`.
Vite proxies `/api` to that address; production uses the same origin for UI and
API requests.

The journal is the initial view. Capture accepts ordinary lines and portable
indented bullets; the complete tree is submitted in one mutation. Existing
bullets have explicit Save actions, and the browser keeps unfinished drafts.
Use `#tags` and `[[references]]` in text, or edit the explicit tag list. A tag
opens a filtered search. The sidebar loads further tag pages on demand, so
large dictionaries remain fully accessible. A reference opens the corresponding subject scope.
Revision history can restore an earlier state as a new revision. Archive a leaf
bullet from its action menu; its history opens immediately with a Restore action.
Bullets with children must have their children moved or archived first.

Keyboard shortcuts: `Cmd/Ctrl K` opens search; `Cmd/Ctrl Shift Enter` captures in
today's journal; `Cmd/Ctrl Enter` saves an active editor. Each bullet's action
menu exposes focus, child/sibling insertion, movement, history, and backlinks.
Stable links use `/#/node/ID`, `/#/subject/ID`, and `/#/journal/YYYY-MM-DD`.

The interface uses the same-origin, HttpOnly session provided by the service.
The instance access token is never put in browser storage. Note drafts are
stored in local storage on this browser; canonical data remains in SQLite.
The app uses no remote fonts or analytics.

Tests cover outline parsing, atomic capture requests, stable reference display,
conflicting and uncertain writes, local draft recovery, lazy branch loading,
pagination, search filters, stale search responses, and session sign-in.
Responsive CSS provides a desktop sidebar, a tablet layout, and a mobile
navigation sheet. No visual or manual inspection is performed by default.

An optional automated browser check exercises the running service at desktop,
tablet, and phone sizes without screenshots or manual inspection. Use a fresh,
disposable database because the scenario creates and edits notes:

```sh
# In another terminal, from the repository root:
go run -tags sqlite_fts5 ./cmd/vrac serve --dev --listen 127.0.0.1:18086 --db /tmp/vrac-browser-test.sqlite

# From web/, install the browser test dependency outside the checkout:
npm install --prefix /tmp/vrac-browser-check playwright
VRAC_PLAYWRIGHT_MODULE=/tmp/vrac-browser-check/node_modules/playwright/index.mjs \
VRAC_BROWSER_BINARY="/path/to/chrome" \
node scripts/browser-check.mjs
```

`VRAC_TEST_URL` overrides the local service address. Omit
`VRAC_BROWSER_BINARY` when using a Playwright-managed Chromium installation.
The scenario checks atomic tree capture, tags and project filtering,
permalink reload, editing and revision restoration, mobile navigation,
JavaScript runtime errors, and horizontal overflow.
