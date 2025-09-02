## Watchlist Notify Frontend

Next.js (App Router) frontend for the Watchlist Notify service.

Status: Early development. APIs and components not stable yet.

### Stack

- Next.js 15 (React 19)
- TypeScript
- Tailwind CSS 4
- ESLint (Next.js + custom config)

### Goals

- Google OAuth initiation (redirect to backend `/auth/google/login`)
- Manage watchlist (CRUD UI) (planned)
- Manage streaming service subscription settings (planned)
- User preferences (notification frequency, email) (planned)
- Responsive, accessible UI

### Project Structure (current minimal)

```
app/
	layout.tsx      # Root layout
	page.tsx        # Landing page
	globals.css     # Tailwind base + app styles
public/           # Static assets
```

### Getting Started

Install dependencies (choose one):

```bash
pnpm install
# or
npm install
# or
yarn install
```

Run dev server:

```bash
pnpm dev
```

Then open http://localhost:3000

### Environment Variables

Frontend currently uses the backend base URL from `.env.local` (create manually). Example:

```bash
NEXT_PUBLIC_API_BASE=http://localhost:8080
```

Add more as features expand (e.g. feature flags, analytics keys).

### Auth Flow (frontend perspective)

1. User clicks "Sign in with Google" button (to be implemented) -> window.location = `${NEXT_PUBLIC_API_BASE}/auth/google/login`
2. Backend sets temporary state cookie & redirects to Google
3. After callback, backend sets `auth_token` HttpOnly cookie (frontend cannot read directly)
4. Client calls `${NEXT_PUBLIC_API_BASE}/api/me` to fetch user profile (credentials include)

Implementation Note: Use `fetch(url, { credentials: 'include' })` for protected endpoints.

### Planned Components

- AuthButton / UserMenu
- WatchlistTable / WatchlistItemForm
- ServiceSelector (checkbox group for streaming services)
- PreferencesForm (notification frequency, email override)
- AvailabilityBadge / StatusIndicators
- Toast / Notification system

### Styling

Tailwind utilities available globally via `globals.css`.
Add component-level styles via CSS Modules or keep utility-first.

### Linting & Formatting

Run lint:

```bash
pnpm lint
```

(Formatter not yet configured; consider Prettier or Biome.)

### API Helpers (planned)

Create a thin wrapper in `lib/api.ts` for:

```ts
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${process.env.NEXT_PUBLIC_API_BASE}${path}`, {
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(init?.headers || {}) },
    ...init,
  });
  if (!res.ok) throw new Error(`API ${res.status}`);
  return res.json();
}
```

### Data Fetching Strategy

- Public pages: Static or dynamic as needed.
- Authenticated pages: Server Components calling the API with cookies (Next.js fetch with `cache: 'no-store'`).
- Consider middleware for auth gating (`/dashboard`, etc.).

### Accessibility

- Use semantic HTML and ARIA roles only when needed.
- Plan to add integration tests for keyboard navigation.

### Testing (planned)

- Unit: React Testing Library + Vitest / Jest (TBD)
- E2E: Playwright (for auth + flows)

### Deployment (Current & Planned)

Current:

- Frontend is deployed on Vercel; production domain `https://watchlistnotify.com` managed via Cloudflare DNS/proxy.
- Cloudflare provides caching / WAF; Vercel handles build & preview deployments.
- API still local; all auth endpoints point to local backend during development.

Planned:

- Introduce `https://api.watchlistnotify.com` for Go backend (CORS allow only production origins).
- MongoDB Atlas cluster for production data (replace local Docker Mongo).
- Environment variables configured in Vercel Project Settings (`NEXT_PUBLIC_*` only exposed client-side).
- Incremental adoption of edge caching for static assets through Cloudflare + Vercel.

Build:

```bash
pnpm build
```

Then Vercel serves optimized output.

### Roadmap (Frontend-Specific)

- [ ] Auth UI + state management
- [ ] Basic watchlist CRUD screens
- [ ] Service subscription selection
- [ ] Preferences page
- [ ] Email notification settings UX
- [ ] Loading & error states standardized
- [ ] Responsive layout + dark mode

### Contributing

Follow root project guidelines. Keep components small and accessible.

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
