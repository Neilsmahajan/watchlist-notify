# Watchlist Notify

Track your personal movie & TV show watchlist across streaming platforms and automatically get email alerts when titles become available on the services you already subscribe to.

> Status: Active development. Implementing Go backend (Gin + MongoDB + Auth0 authentication) and a Next.js frontend with modern UI. TMDb integration (search + watch providers) implemented. Expect breaking changes until v0.1.0.

## Table of Contents

1. Vision & Features
2. High-Level Architecture
3. Tech Stack
4. Local Development (Backend & Frontend)
5. Environment Variables
6. Authentication Flow (Auth0 + JWT Cookie)
7. API (current + planned)
8. Data Model
9. Background Jobs (planned)
10. Email Delivery (planned)
11. Testing & Tooling
12. Make Targets Cheat Sheet
13. Deployment (Current & Planned)
14. Roadmap / Milestones
15. Contributing
16. Maintainer & Contact
17. License

## 1. Vision & Features

Problem: You maintain a list of movies / shows you want to watch, but you rarely know when they land on the services you already pay for.

Solution: Centralize your watchlist + subscription list and periodically notify you (email) when new items become streamable—reducing time spent searching and avoiding rental duplicates.

Core (in-progress):

- Auth0 authentication (Google OAuth + username/password login)
- Modern responsive UI with navigation, dashboard, and user management
- Manage user profile (email, display name, picture)
- Persist user watchlist items (title, type, year, external IDs)
- Persist user streaming service subscriptions (Netflix, Max, Prime Video, etc.)
- TMDb integration for content search and watch provider data
- Display availability of watchlist items across the user's streaming subscriptions in the frontend
- Periodic availability scan (scheduled job) -> email digest
- User preferences: notification frequency, primary email

Future / Stretch:

- Region-specific availability (country codes)
- Multiple notification channels (push / SMS)
- Per-item instant alert toggle
- Browser extension for 1-click adding
- Public shareable lists / collaborative lists
- AI-based alternative title matching / fuzzy matching

## 2. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Cloudflare (DNS/Proxy/CDN)            │
│  watchlistnotify.com  (A / CNAME -> Vercel + future API)     │
└─────────────────────────────────────────────────────────────┘
			  │
			  │ HTTPS
			  ▼
	 ┌────────────────────┐          ┌──────────────────────────┐
	 │  Frontend (Vercel) │  API     │      Backend API (Go)    │
	 │  Next.js App Router│ <──────> │ Gin REST + Auth + Jobs   │
	 └──────────┬─────────┘          └──────────┬──────────────┘
			  │                               │
			  │ (public assets, auth start)   │ MongoDB Atlas (Users, Watchlist, etc.)
			  │                               ▼
			  │                       ┌────────────────┐
			  │                       │ MongoDB Atlas   │
			  │                       └────────────────┘
			  │                               │
			  │                               ▼ (planned)
			  │                     Availability Providers (APIs)
			  │                     • TMDb (search + watch/providers) [planned next]
			  │                               │
			  │                               ▼
			  │                        Email Provider (planned)
			  ▼
		  User Browser
```

Current Hosting:

- Frontend deployed on Vercel, served at https://watchlistnotify.com via Cloudflare DNS / proxy.
- Domain & TLS termination proxied by Cloudflare (Vercel also manages certs for the connected domain; Cloudflare in proxy mode supplies edge caching & security).
- Backend (Go API) currently local / in development; will receive a subdomain (e.g. `api.watchlistnotify.com`) or path-based routing through Cloudflare once deployed.
- Database will migrate to MongoDB Atlas (multi-region / backups) from local Docker during later milestones.

## 3. Tech Stack

Backend:

- Go (Gin web framework)
- MongoDB (official Go driver)
- Auth0 (authentication with multiple providers)
- JWT (HMAC) stored in HttpOnly cookie
- Docker / Docker Compose for local DB
- TMDb API (search + watch providers for availability)

Frontend:

- Next.js 15 (App Router, React 19)
- Auth0 Next.js SDK for seamless authentication
- Tailwind CSS 4 with modern responsive design
- Professional UI with navigation, dashboard, search, and settings pages
- TypeScript with ESLint configuration
- Optimized images with Next.js Image component

Infrastructure (planned):

- Reverse proxy (e.g. Caddy / Nginx / Cloudflare)
- Background worker (Go) or cron (Cloud Scheduler / GitHub Actions)
- Email provider (e.g. SendGrid / Postmark / AWS SES)

## 4. Local Development

Clone the repo:

```bash
git clone https://github.com/neilsmahajan/watchlist-notify.git
cd watchlist-notify
```

Start MongoDB (Compose):

```bash
make docker-run
```

Create a `.env` file in the project root (see variables below) then run backend:

```bash
make run
# or with live reload (installs air if missing)
make watch
```

Frontend (in separate terminal):

```bash
cd frontend
pnpm install # or npm/yarn
pnpm dev
```

Visit: http://localhost:3000

Health endpoint (backend default):

```
GET http://localhost:8080/health
```

## 5. Environment Variables

Create `.env` in repository root (never commit secrets). A starter `.example.env` is provided:

```bash
cp .example.env .env
```

Then edit secrets. Example (adjust as needed):

```env
# Backend
PORT=8080
BASE_URL=http://localhost:8080
FRONTEND_URL=http://localhost:3000
JWT_SECRET=replace-with-long-random-string

# Auth0 (from Auth0 Dashboard Application settings)
AUTH0_DOMAIN=your-domain.auth0.com
AUTH0_CLIENT_ID=your-auth0-client-id
AUTH0_CLIENT_SECRET=your-auth0-client-secret
AUTH0_AUDIENCE=https://api.watchlistnotify.com

# Mongo
DB_IMAGE=mongo:8.0.13
DB_HOST=localhost
DB_PORT=27017
DB_USERNAME=devuser           # optional
DB_ROOT_PASSWORD=devpassword  # optional
DB_DATABASE=watchlist_notify

# TMDb
TMDB_API_KEY=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
# Optional defaults (keep or set in code):
# TMDB_API_BASE=https://api.themoviedb.org/3
# TMDB_REGION=US

# Email (planned)
EMAIL_FROM=watchlist@watchlistnotify.com
EMAIL_PROVIDER_API_KEY=xxx
```

Frontend environment variables (create `frontend/.env.local`):

```env
# Auth0 configuration for Next.js
AUTH0_SECRET='long-random-string-for-session-encryption'
AUTH0_DOMAIN='https://your-domain.auth0.com'
AUTH0_CLIENT_ID='your-auth0-client-id'
AUTH0_CLIENT_SECRET='your-auth0-client-secret'
AUTH0_AUDIENCE='https://api.watchlistnotify.com'
AUTH0_SCOPE='openid profile email'

# Backend API URL for server-side requests
BACKEND_URL='http://localhost:8080'
NEXT_PUBLIC_AUTH0_AUDIENCE='https://api.watchlistnotify.com'
```

## 6. Authentication Flow (Auth0 + JWT Cookie)

1. Frontend redirects to Auth0 Universal Login via `/auth/login`
2. User authenticates with Auth0 (Google OAuth, username/password, etc.)
3. Auth0 redirects to `/auth/callback` with authorization code
4. Backend exchanges code for tokens and user info, creates/updates user in MongoDB
5. Backend issues signed JWT stored in HttpOnly cookie for API access
6. Frontend can call protected endpoints (e.g. `GET /api/me`) with automatic cookie inclusion

Auth0 Configuration:

- Regular Web Application with server-side token exchange
- Custom API audience for JWT token validation
- Post-login Action adds custom claims (email, name, picture) to access tokens
- Support for multiple identity providers (Google, username/password)

Security Notes:

- HttpOnly cookies prevent XSS token theft
- Auth0 manages OAuth flows and security best practices
- Custom claims allow backend to extract user info from JWT without additional API calls
- CSRF protection via Auth0's state parameter and SameSite cookie attributes

## 7. API

Implemented:

- `GET /health` – service health
- `GET /api/me` – authenticated user document
- `PATCH /api/me/preferences` – update user preferences (email, name, notification prefs)
- `GET /api/me/services` – list user streaming services
- `PATCH /api/me/services` – update user streaming services
- `POST /api/watchlist` – add item
- `GET /api/watchlist` – list items
- `DELETE /api/watchlist/:id`
- `PUT /api/watchlist/:id` – update metadata / status
- `GET /api/search` – search titles via TMDb
- `GET /api/availability` – get availability data for watchlist items

Near-term planned:

- `GET /api/availability/:id?type=movie|tv` – get provider availability for a specific title
- `GET /api/me/availability` – aggregate availability for the user's entire watchlist (paginated)
- Background job endpoints for availability scanning
- Email notification preferences and digest endpoints

Error Format (current):

```json
{ "error": "message" }
```

Will evolve to structured error codes.

## 8. Data Model (current minimal)

`User` (Mongo `users` collection):

```json
{
  "_id": "ObjectId",
  "auth0_id": "string",
  "email": "string",
  "name": "string",
  "picture": "url",
  "created_at": "ISO8601",
  "updated_at": "ISO8601"
}
```

`WatchlistItem` (Mongo `watchlist_items` collection):

```json
{
  "_id": "ObjectId",
  "user_id": "ObjectId",
  "tmdb_id": "number",
  "title": "string",
  "type": "movie|tv",
  "year": "number",
  "poster_path": "string",
  "added_at": "ISO8601",
  "status": "want_to_watch|watching|watched"
}
```

Planned collections: `services`, `availability_cache`, `jobs`.

## 9. Background Jobs (planned)

- Nightly availability scan per user service set
- Deduplicate API lookups by batching unique title/provider queries
- Store availability snapshot to avoid redundant external calls

## 10. Email Delivery (planned)

- Digest summarizing new items available since last notification
- Optional per-item immediate alert
- Template engine (Go text/template or MJML via build)

## 11. Testing & Tooling

Run all tests:

```bash
make test
```

Integration DB tests only:

```bash
make itest
```

Linting (add later): golangci-lint (planned), frontend ESLint already present.

API requests (Bruno):

- Collection lives at `docs/bruno/`. Open that folder in Bruno to load requests.
- Use `docs/bruno/environments/local.example.bru` as a template to create `local.bru` (ignored). Set `base_url` (e.g., `http://localhost:8080`) and `token`.
- Auth is configured at the `API` folder level using `{{token}}`. Get a token via Auth0 login flow or copy from browser dev tools after authentication.
- All request URLs use `{{base_url}}`; path params can be set as variables (e.g., `{{id}}`).

## 12. Make Targets Cheat Sheet

| Target           | Description                             |
| ---------------- | --------------------------------------- |
| make all         | build + test                            |
| make build       | compile Go binary -> `./main`           |
| make run         | run API (no reload)                     |
| make watch       | run with Air live reload (auto-install) |
| make docker-run  | start Mongo via compose                 |
| make docker-down | stop Mongo                              |
| make test        | run all Go tests                        |
| make itest       | run integration tests (database)        |
| make clean       | remove binary                           |

## 13. Deployment (Current & Planned)

Current State:

- Frontend: Deployed on Vercel (production) mapped to `watchlistnotify.com` through Cloudflare (CNAME flattening/ALIAS depending on config). Preview deployments auto-generated by Vercel.
- DNS / CDN / Security: Cloudflare manages DNS records (`A`/`CNAME`) and provides caching + WAF layer. Keep CORS origin list tight (production: `https://watchlistnotify.com`).
- Backend: Not yet deployed; running locally during development. All auth flows redirect to local backend until production API domain exists.
- Database: Local Docker Mongo for dev. Production target: MongoDB Atlas cluster (dedicated or M0+ tier initially).

Planned Backend Deployment Options:

1. Container on a managed platform (Fly.io, Render, Railway) with health checks.
2. Kubernetes (longer-term) behind Cloudflare tunnel or load balancer.
3. Vercel Serverless / Edge not used for Go API (needs persistent cron + background tasks) – prefer dedicated container.

Production Domain Layout (planned):

- `https://watchlistnotify.com` – Next.js frontend (Vercel)
- `https://api.watchlistnotify.com` – Go API (CORS restricted)
- Optional future: `https://img.watchlistnotify.com` for image optimization / CDN offload.

Environment Variables (Prod Guidance):

- Set frontend secrets / public config in Vercel Project Settings (mask sensitive keys; only expose `NEXT_PUBLIC_*`).
- Store backend secrets (OAuth, JWT secret, DB URI) in platform secret manager (not in repo). Use Atlas SRV connection string with proper user.

MongoDB Atlas Setup (planned):

- Create project & cluster (region near majority users).
- Define database user with least privileges.
- Enable IP access (platform egress IPs) or set up VPC peering.
- Add indexes after schema stabilizes (watchlist item queries by user + availability state).

Security / Observability Roadmap:

- Add structured logging (Zap or Zerolog) with request IDs.
- Add metrics endpoint (Prometheus) behind auth / internal.
- Add Cloudflare security rules (bot fight, rate limiting for auth endpoints).
- Add error tracking (Sentry) & performance monitoring (OpenTelemetry) later.

Scaling Considerations:

- Cache external provider responses (Redis or in-memory with TTL) to reduce API costs.
- Batch availability lookups via job scheduler (cron + queue) to flatten burst load.

Deployment Checklist (API – future):

1. Build image & push (tag with git SHA)
2. Apply infra config (secrets, env, DB URL)
3. Run migrations / ensure indexes (script TBD)
4. Smoke test `/health` via Cloudflare
5. Flip DNS for `api.watchlistnotify.com` (low TTL) or enable route in Cloudflare
6. Monitor logs & metrics for 24h

## 14. Roadmap / Milestones

- [x] Auth0 integration with modern UI
- [x] Basic watchlist CRUD functionality
- [x] TMDb integration for search and availability
- [x] Professional frontend with navigation, dashboard, and user management
- [ ] v0.1.0 MVP: Email notifications + availability scanning job
- [ ] v0.2.0 Enhanced availability integration (region support + more providers)
- [ ] v0.3.0 Digest email + advanced preferences
- [ ] v0.4.0 Multiple notification channels + browser extension
- [ ] v0.5.0 Public beta (stability + docs)

## 15. Contributing

PRs welcome after public beta. Until then, issues for ideas / architecture discussion are appreciated.

Local Dev Tips:

- Use `air` for fast reload
- Keep Mongo indexes minimal until schema stabilizes
- Add fake data scripts (planned `cmd/seed`)

## 16. Maintainer & Contact

Maintainer: Neil Mahajan  
Links & Site: https://links.neilsmahajan.com  
Personal Email: neilsmahajan@gmail.com  
Project Contact: contact@watchlistnotify.com

Security Disclosure: Email both addresses with subject `[SECURITY] <short summary>` and allow time for remediation before public disclosure.

## 17. License

Licensed under the MIT License. See `LICENCE` for the full text.

SPDX-License-Identifier: MIT

---

Questions / suggestions? Open an issue.
