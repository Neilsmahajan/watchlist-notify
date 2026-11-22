## Watchlist Notify Frontend

The Next.js frontend of Watchlist Notify: one watchlist, checked every day
against the streaming services you already pay for.

### Stack

- Next.js 15 (App Router, React 19), TypeScript (strict)
- Tailwind CSS 4, with the palette defined as CSS variables in `app/globals.css`
- Auth0 Next.js SDK v4 for sign-in (`lib/auth0.ts`, `middleware.ts`)
- pnpm

### Pages

| Address | What it is |
| --- | --- |
| `/` | Landing page when signed out, your dashboard when signed in |
| `/demo` | A public, read-only sample watchlist (no sign-in) |
| `/watchlist` | Your list as a poster grid with filters, paging and a detail view |
| `/search` | Search TMDB and add titles |
| `/settings` | Services, email digest, IMDb link and CSV import, theme, account |
| `/profile` | Account details and counts |
| `/contact`, `/privacy`, `/terms` | Static pages |

### How it is built

```
app/                  pages, plus api/ routes that proxy to the Go API
components/
  home/               landing page and signed-in dashboard
  watchlist/          grid, filter bar, tile and detail dialog (shared with /demo)
  settings/           one file per settings section
  Poster.tsx          TMDB poster with srcset, or a designed fallback tile
  ServiceLogo.tsx     streaming service logo, or its initials
lib/
  api/backend.ts      server-side calls to the Go API (session token attached)
  api/proxy.ts        what the app/api routes share
  watchlist/query.ts  filters <-> URL <-> API query
  format.ts           dates and runtimes, formatted the same on server and client
  theme.ts            the theme script and helpers
scripts/              a mock API for local work (not part of the build)
```

**Data flow.** Pages are server components. They read the session, call the
Go API directly and render the first 48 titles; the browser asks for more
through the `app/api` proxy routes as you scroll. Filters, search and sort
live in the address, so a filtered view can be shared and the back button
works. The dashboard and the profile ask the API for counts
(`meta.total`, `meta.facets`) and a few rows; nothing loads the whole list.

**Images.** Posters and service logos come straight from TMDB's image CDN in
the size the layout needs (`w185`, `w342`, `w500` with `srcset`). They do not
go through the Vercel image optimizer, which is why plain `<img>` is used.

**Theme.** Light and dark are two sets of CSS variables. A small inline
script sets `data-theme` on `<html>` before first paint from the saved
choice; with no saved choice the system setting decides. The theme is not
React state, so it cannot cause a flash or a hydration mismatch.

**Dates.** Anything rendered on the server and again in the browser is
formatted with a fixed locale and time zone (`lib/format.ts`). Formatting
with the runtime's own locale is what used to cause React hydration errors.

### The demo

`/demo` reads `GET /api/demo/watchlist` and `GET /api/demo/services` from the
Go API, which serve one seeded sample account, read-only. The page is cached
for five minutes. If the API has no demo account yet, the page says so and
the landing page falls back to a built-in set of posters.

### Getting started

```bash
pnpm install
pnpm dev
```

Then open http://localhost:3000. Copy `.env.example` to `.env.local` and fill
in the Auth0 values; `BACKEND_URL` points at the Go API
(`http://localhost:8080` locally).

To work on the interface without Auth0 or the Go API:

```bash
node scripts/mock-api.mjs                      # a fake API on port 8787
BACKEND_URL=http://localhost:8787 pnpm dev     # in another terminal
AUTH0_SECRET=<the dev server's throwaway secret> node scripts/mock-session.mjs
```

The last command prints a value for the `__wl_session` cookie, which gives a
signed-in view on localhost. Use a throwaway `AUTH0_SECRET` for this, never a
real one.

`scripts/mock-api.mjs` lists its switches at the top (empty account, no demo,
no IMDb routes, slow responses).

### Checks

```bash
pnpm lint
pnpm typecheck
pnpm build
```

### Deployment

Vercel builds and deploys `main` to https://watchlistnotify.com; pull requests
get a preview. Environment variables live in the Vercel project settings.

### Maintainer & Contact

Maintainer: Neil Mahajan  
Links & Site: https://links.neilsmahajan.com  
Personal Email: neilsmahajan@gmail.com  
Project Contact: contact@watchlistnotify.com

### License

MIT License – see root `LICENCE` file.

SPDX-License-Identifier: MIT

---

Questions / ideas? Open an issue in the main repo.
