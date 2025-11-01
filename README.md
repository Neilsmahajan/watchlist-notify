# Watchlist Notify

Track your personal movie & TV show watchlist across streaming platforms and automatically get email alerts when titles become available on the services you already subscribe to.

> Status: Beta (v0.9). Core features complete with Go backend (Gin + MongoDB + Auth0), Next.js frontend, TMDb integration, and scheduled digest email notifications via Postmark. Production-ready and deployed on Cloud Run + Vercel.

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
14. Deployment
15. Roadmap / Milestones
16. Contributing
17. Maintainer & Contact
18. License

## 1. Vision & Features

Problem: You maintain a list of movies / shows you want to watch, but you rarely know when they land on the services you already pay for.

Solution: Centralize your watchlist + subscription list and periodically notify you (email) when new items become streamable—reducing time spent searching and avoiding rental duplicates.

Core Features (Implemented):

- ✅ Auth0 authentication (Google OAuth + username/password login)
- ✅ Modern responsive UI with navigation, dashboard, and user management
- ✅ User profile management (email, display name, picture)
- ✅ Watchlist management (add, update, delete items with TMDb metadata)
- ✅ Streaming service subscriptions (Netflix, Max, Prime Video, Apple TV+, etc.)
- ✅ TMDb integration for content search and watch provider data
- ✅ Real-time availability checking across user's subscribed services
- ✅ **Scheduled digest email notifications** (configurable frequency: daily, weekly, monthly)
- ✅ **Notification preferences** with customizable intervals and email selection
- ✅ **Test email functionality** to preview digest emails
- ✅ Redis caching for TMDb API responses (Upstash)
- ✅ Production deployment on Cloud Run (API) and Vercel (Frontend)

Future Enhancements:

- Region-specific availability (international support)
- Multiple notification channels (push notifications, SMS)
- Per-item instant alert toggle
- Browser extension for 1-click adding from streaming sites
- Public shareable lists / collaborative watchlists
- AI-based content recommendations
- Advanced filtering and sorting options

## 2. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ Cloudflare (DNS/Proxy/CDN)                                  │
│ watchlistnotify.com (A / CNAME -> Vercel + Cloud Run API)   │
└─────────────────────────────────────────────────────────────┘
                │
┌───────────────┴────────────────┐
│                                │
▼                                ▼
┌──────────────────┐   ┌──────────────────┐
│ Vercel (Next.js) │   │ Cloud Run (API)  │
│ Frontend         │◄─►│ Go Backend       │
└──────────────────┘   └────┬─────────────┘
                            │
    ┌───────────────────────┼───────────────────────┐
    │                       │                       │
    ▼                       ▼                       ▼
┌──────────────────┐   ┌──────────────┐   ┌────────────────────┐
│ MongoDB Atlas    │   │Upstash Redis │   │ Cloud Run Job      │
│ (Database)       │   │ (Cache)      │   │ (Digest Worker)    │
└──────────────────┘   └──────────────┘   └─────────┬──────────┘
                           │
                      Triggered by
                           │
                           ▼
                      ┌────────────────────┐
                      │ Cloud Scheduler    │
                      │ (Hourly Cron)      │
                      └─────────┬──────────┘
                                │
                          Sends emails via
                                ▼
                      ┌────────────────────┐
                      │ Postmark API       │
                      │ (Email Delivery)   │
                      └────────────────────┘
```

Current Hosting:

- **Frontend**: Deployed on Vercel at https://watchlistnotify.com via Cloudflare DNS/proxy
- **API**: Deployed on Google Cloud Run at `api.watchlistnotify.com` (or via subdomain)
- **Digest Worker**: Cloud Run Job triggered hourly by Cloud Scheduler
- **Database**: MongoDB Atlas (multi-region with automated backups)
- **Cache**: Upstash Redis (managed) for TMDb API responses with graceful fallback
- **Email**: Postmark for transactional emails and digest notifications
- **Domain & TLS**: Managed by Cloudflare with edge caching and DDoS protection

## 3. Tech Stack

Backend:

- Go 1.25+ (Gin web framework)
- MongoDB (official Go driver)
- Redis (Upstash managed) via go-redis client with graceful fallback
- Auth0 (multi-provider authentication)
- JWT (HMAC) stored in HttpOnly cookies
- MongoDB Atlas for database (local Docker optional)
- TMDb API (search + watch providers)
- Postmark API (email delivery)
- Cloud Run (API deployment)
- Cloud Run Jobs + Cloud Scheduler (digest worker)

Frontend:

- Next.js 15 (App Router, React 19)
- Auth0 Next.js SDK for seamless authentication
- Tailwind CSS 4 with modern responsive design
- Professional UI with navigation, dashboard, search, and settings pages
- TypeScript with ESLint configuration
- Optimized images with Next.js Image component

Infrastructure:

- Google Cloud Run (API service deployment)
- Google Cloud Run Jobs (digest worker)
- Google Cloud Scheduler (hourly digest trigger)
- Google Artifact Registry (container images)
- Upstash Redis (managed cache)
- Postmark (email delivery service)
- Vercel (frontend hosting)
- Cloudflare (DNS, CDN, DDoS protection)

## 4. Local Development

Clone the repo:

```bash
git clone https://github.com/neilsmahajan/watchlist-notify.git
cd watchlist-notify
```

Configure MongoDB (Atlas – preferred):

1. Create or use an existing MongoDB Atlas cluster.
2. Allow your IP (Network Access) and create a database user with password.
3. Copy your connection string (SRV or standard), e.g.:
   - mongodb+srv://<user>:<pass>@<cluster>/?retryWrites=true&w=majority
4. Set MONGODB_URI in your `.env` (see Environment Variables below). The backend will automatically create required indexes on startup.

Optional: start a local MongoDB via Docker Compose (only if you are not using Atlas):

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
# Preferred: Atlas connection string. If set, this takes precedence over the DB_* values below.
MONGODB_URI="mongodb+srv://dev_user:strongpassword@your-cluster.mongodb.net/?retryWrites=true&w=majority"

# Fallback local development variables (used only when MONGODB_URI is not set)
DB_IMAGE=mongo:8.0.13          # optional, for docker-compose only
DB_HOST=localhost
DB_PORT=27017
DB_USERNAME=devuser            # optional
DB_ROOT_PASSWORD=devpassword   # optional
DB_DATABASE=watchlistnotify

# Cache
REDIS_URL=rediss://default:examplepassword@your-upstash-endpoint:6379
# Set to "disabled" to bypass Redis during local development

# TMDb
TMDB_API_KEY=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
# Optional defaults (keep or set in code):
# TMDB_API_BASE=https://api.themoviedb.org/3
# TMDB_REGION=US

# Email (production)
POSTMARK_SERVER_TOKEN=your-postmark-server-token
POSTMARK_BASE_URL=https://api.postmarkapp.com/email
EMAIL_FROM=alerts@watchlistnotify.com
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

## 7. API Documentation

### Quick Reference

**Interactive Documentation (Swagger UI):**

- Production: `https://api.watchlistnotify.com/swagger/index.html`
- Local: `http://localhost:8080/swagger/index.html`

**OpenAPI Specification:**

- JSON: `https://api.watchlistnotify.com/swagger/doc.json`
- YAML: `/docs/swagger.yaml` (in repository)

**Complete API Guide:**
See [`docs/API.md`](docs/API.md) for comprehensive documentation including:

- Authentication details
- Request/response examples
- Error handling
- Pagination
- Rate limiting (planned)

### Endpoint Summary

#### Authentication Endpoints

- `GET /health` – Service health check (public)

#### User Endpoints

- `GET /api/me` – Get authenticated user profile
- `PATCH /api/me/preferences` – Update user preferences (email, name, digest settings)
- `GET /api/me/services` – List user's streaming service subscriptions
- `PATCH /api/me/services` – Update streaming service subscriptions
- `POST /api/me/notifications/test` – Send test digest email

#### Watchlist Endpoints

- `POST /api/watchlist` – Add item to watchlist
- `GET /api/watchlist` – List user's watchlist items with filters and pagination
- `PATCH /api/watchlist/:id` – Update item metadata/status
- `DELETE /api/watchlist/:id` – Remove item from watchlist
- `POST /api/watchlist/import` – Bulk import from CSV (IMDb format)

#### Content Discovery

- `GET /api/search` – Search movies/TV shows via TMDb
- `GET /api/availability/:id` – Get streaming availability for specific title
- `POST /api/availability/batch` – Batch availability check (max 500 items)

### Authentication

All protected endpoints require an Auth0 JWT token in the `Authorization` header:

```http
Authorization: Bearer YOUR_JWT_TOKEN
```

### Error Format

All errors return a consistent JSON structure:

```json
{ "error": "Descriptive error message" }
```

### Generating API Documentation

If you modify handlers or add new endpoints, regenerate the OpenAPI spec:

```bash
# Install swag CLI (first time only)
go install github.com/swaggo/swag/cmd/swag@latest

# Generate documentation
make swagger

# Or manually
swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal
```

The generated files (`docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`) are auto-generated and git-ignored.

## 8. Data Model

### User Collection (`users`)

```json
{
  "_id": "ObjectId",
  "auth0_id": "auth0|123456",
  "email": "user@example.com",
  "name": "John Doe",
  "picture": "https://...",
  "notification_email": "custom@example.com",
  "services": ["netflix", "hulu", "prime_video"],
  "digest_settings": {
    "enabled": true,
    "interval": 7,
    "interval_unit": "days",
    "last_sent_at": "2025-01-15T10:00:00Z",
    "next_scheduled_at": "2025-01-22T10:00:00Z"
  },
  "digest_consent": true,
  "created_at": "2025-01-01T12:00:00Z",
  "updated_at": "2025-01-15T10:00:00Z"
}
```

### Watchlist Item Collection (`watchlist_items`)

```json
{
  "_id": "ObjectId",
  "user_id": "ObjectId",
  "tmdb_id": 12345,
  "title": "The Matrix",
  "type": "movie",
  "year": 1999,
  "poster_path": "/poster.jpg",
  "added_at": "2025-01-10T15:30:00Z",
  "status": "want_to_watch"
}
```

**Status Values**: `want_to_watch`, `watching`, `watched`

## 9. Email Notifications

### Architecture

The digest email system uses a Cloud Run Job triggered hourly by Cloud Scheduler:

1. **Cloud Scheduler** triggers the digest worker every hour
2. **Digest Worker** (Cloud Run Job) executes:
   - Queries users with `next_scheduled_at <= now` and `digest_consent = true`
   - For each eligible user:
     - Fetches their watchlist items
     - Checks for newly available content on their subscribed services
     - Generates personalized HTML email
     - Sends via Postmark API
     - Updates `last_sent_at` and recalculates `next_scheduled_at`

### User Preferences

Users can configure:

- **Email address**: Use Auth0 account email or custom email
- **Digest frequency**: Every X days/weeks/months (e.g., every 7 days, every 2 weeks)
- **Consent**: Enable/disable digest emails
- **Test email**: Preview digest without changing schedule

### Email Content

Digest emails include:

- Newly available items with streaming service badges
- Watchlist summary (total items, available count, watching count)
- Call-to-action button to view full watchlist
- Professional HTML template with responsive design
- Unsubscribe link in footer

### Deployment

The digest worker is deployed as a Cloud Run Job:

```bash
# Deploy worker
./scripts/release-worker.sh --env-file cloudrun.env

# Manual execution (testing)
gcloud run jobs execute digest-worker --region us-east1

# View logs
gcloud logging read "resource.type=cloud_run_job AND resource.labels.job_name=digest-worker" --limit 50
```

See `cmd/digest-worker/README.md` for detailed deployment and monitoring instructions.

## 10. Testing & Tooling

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

## 11. Make Targets Cheat Sheet

| Target                 | Description                             |
| ---------------------- | --------------------------------------- |
| make all               | build + test                            |
| make build             | compile Go binary -> `./main`           |
| make run               | run API (no reload)                     |
| make watch             | run with Air live reload (auto-install) |
| make swagger           | generate OpenAPI/Swagger docs           |
| make docker-run        | start Mongo via compose                 |
| make docker-down       | stop Mongo                              |
| make test              | run all Go tests                        |
| make itest             | run integration tests (database)        |
| make clean             | remove binary                           |
| make docker-auth       | configure Docker auth for Artifact Reg. |
| make docker-build      | build container (linux/amd64)           |
| make docker-push       | push image to Artifact Registry         |
| make docker-build-push | build and push in one step              |
| make run-container     | run the image locally                   |
| make deploy            | deploy API to Cloud Run                 |
| make deploy-worker     | deploy digest worker to Cloud Run Job   |

Note: The docker targets are optional if you configure MONGODB_URI to use MongoDB Atlas.

## 12. Deployment

### Frontend (Vercel)

The Next.js frontend is deployed on Vercel with automatic deployments:

- **Production**: https://watchlistnotify.com (auto-deploys from `main` branch)
- **Preview**: Automatic preview deployments for pull requests
- **Domain**: Managed via Cloudflare DNS with CNAME to Vercel

Configuration:

- Environment variables set in Vercel dashboard
- Build command: `cd frontend && pnpm install && pnpm build`
- Output directory: `frontend/.next`

### Backend API (Cloud Run)

The Go API is deployed to Google Cloud Run:

```bash
# Build and deploy API
./scripts/release.sh --env-file cloudrun.env

# Or with Cloud Build
./scripts/release.sh --cloud-build --env-file cloudrun.env
```

**Environment variables** are loaded from `cloudrun.env` (not committed to git).

**Deployment details**:

- Region: `us-east1`
- Container: Built with Docker, pushed to Artifact Registry
- Scaling: Min 0, Max 10 instances
- Memory: 512Mi, CPU: 1
- Authentication: Public endpoints + JWT-protected routes

### Digest Worker (Cloud Run Job)

The digest email worker runs as a Cloud Run Job:

```bash
# Deploy worker
./scripts/release-worker.sh --env-file cloudrun.env

# Create Cloud Scheduler job (one-time setup)
gcloud scheduler jobs create http digest-worker-hourly \
  --location us-east1 \
  --schedule "0 * * * *" \
  --uri "https://us-east1-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/PROJECT_ID/jobs/digest-worker:run" \
  --http-method POST \
  --oauth-service-account-email "digest-scheduler@PROJECT_ID.iam.gserviceaccount.com"
```

**Monitoring**:

```bash
# View job executions
gcloud run jobs executions list --job digest-worker --region us-east1

# View logs
gcloud logging read "resource.type=cloud_run_job AND resource.labels.job_name=digest-worker" --limit 50

# Manual execution (testing)
gcloud run jobs execute digest-worker --region us-east1
```

See `cmd/digest-worker/README.md` for detailed deployment guide.

### Infrastructure Components

- **MongoDB Atlas**: Managed database with automated backups
- **Upstash Redis**: Managed cache for TMDb API responses
- **Postmark**: Email delivery service
- **Cloudflare**: DNS, CDN, and DDoS protection
- **Google Artifact Registry**: Container image storage

### Key Files

- `cmd/api/main.go` - API server entry point
- `cmd/digest-worker/main.go` - Digest worker entry point
- `internal/server/` - HTTP handlers and routing
- `internal/database/` - MongoDB repositories
- `internal/digest/` - Email generation logic
- `internal/notifications/` - Postmark integration
- `scripts/release.sh` - API deployment script
- `scripts/release-worker.sh` - Worker deployment script
- `Dockerfile` - API container image
- `Dockerfile.worker` - Worker container image
- `frontend/` - Next.js application
- DNS / CDN / Security: Cloudflare manages DNS records (`A`/`CNAME`) and provides caching + WAF layer. Keep CORS origin list tight (production: `https://watchlistnotify.com`).
- Backend: Not yet deployed; running locally during development. All auth flows redirect to local backend until production API domain exists.
- Database: MongoDB Atlas for both dev and prod (recommended). Optional local Docker for offline development.

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

- Cache external provider responses (Upstash Redis) to reduce TMDb API churn; falls back to no-op cache if Redis is unreachable.
- Batch availability lookups via job scheduler (cron + queue) to flatten burst load.

Deployment Checklist (API – future):

1. Build image & push (tag with git SHA)
2. Apply infra config (secrets, env, DB URL, REDIS_URL, Auth0 credentials)
3. Run migrations / ensure indexes (script TBD)
4. Smoke test `/health` via Cloudflare
5. Flip DNS for `api.watchlistnotify.com` (low TTL) or enable route in Cloudflare
6. Monitor logs & metrics for 24h
7. Prime cache and execute an internal notification dry run once email delivery ships

## 13. Roadmap / Milestones

**Completed (v0.9 - Beta)**:

- ✅ Auth0 integration with Google OAuth and username/password login
- ✅ Modern responsive UI with navigation, dashboard, search, profile, and settings
- ✅ Complete watchlist CRUD functionality with TMDb metadata
- ✅ TMDb integration for search and streaming availability
- ✅ User profile and streaming service subscription management
- ✅ Scheduled digest email notifications (configurable frequency)
- ✅ Notification preferences with custom email and interval settings
- ✅ Test email functionality
- ✅ Production deployment (Cloud Run + Vercel)
- ✅ Redis caching for TMDb API responses

**Planned Future Enhancements**:

- [ ] v1.0.0: Public launch with polished onboarding flow
- [ ] v1.1.0: Region-specific availability (international support)
- [ ] v1.2.0: Per-item instant alerts (notify immediately when available)
- [ ] v1.3.0: Browser extension for 1-click watchlist adding
- [ ] v1.4.0: Public shareable lists and collaborative watchlists
- [ ] v1.5.0: Advanced filtering, sorting, and AI-based recommendations
- [ ] v2.0.0: Mobile apps (iOS/Android)

## 14. Contributing

Contributions are welcome! Please open an issue first to discuss major changes.

**Development Workflow**:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes with tests
4. Run tests (`make test`)
5. Commit with clear messages (`git commit -m 'Add amazing feature'`)
6. Push to your branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

**Code Guidelines**:

- Follow Go best practices and conventions
- Write tests for new features
- Update documentation for API changes
- Use meaningful variable and function names
- Keep functions small and focused

**Local Development**:

- Use `make watch` for live reload during development
- Test API endpoints with Bruno collection in `docs/bruno/`
- Check logs for errors before submitting PR

## 15. Maintainer & Contact

Maintainer: Neil Mahajan  
Links & Site: https://links.neilsmahajan.com  
Personal Email: neilsmahajan@gmail.com  
Project Contact: contact@watchlistnotify.com

Security Disclosure: Email both addresses with subject `[SECURITY] <short summary>` and allow time for remediation before public disclosure.

## 16. License

Licensed under the MIT License. See `LICENCE` for the full text.

SPDX-License-Identifier: MIT

---

Questions / suggestions? Open an issue.

## Appendix: Dockerfile, Apple Silicon, and Cloud Run Deployment

This repository includes a production-ready multi-stage `Dockerfile` at the root that builds the Go API from `cmd/api` into a small, distroless image exposing port 8080.

Apple Silicon (M1/M2) note:

- Cloud Run runs linux/amd64 images. On Apple Silicon, use Docker Buildx to build with `--platform linux/amd64`.
- The Makefile includes convenient targets for building and deploying.

Quick steps

1. Authenticate and set defaults

- gcloud auth login
- gcloud config set project <PROJECT_ID>
- make docker-auth PROJECT_ID=<PROJECT_ID> REGION=us-central1

2. Build and push image (Apple Silicon friendly)

- make docker-build-push PROJECT_ID=<PROJECT_ID> REGION=us-central1 REPO=watchlist IMAGE=api

3. Deploy to Cloud Run

- make deploy PROJECT_ID=<PROJECT_ID> REGION=us-central1 REPO=watchlist IMAGE=api SERVICE=watchlist-api

Required environment variables (configure on Cloud Run):

- BASE_URL=https://api.watchlistnotify.com
- FRONTEND_URL=https://watchlistnotify.com
- JWT_SECRET=...
- AUTH0_DOMAIN=...
- AUTH0_CLIENT_ID=...
- AUTH0_CLIENT_SECRET=...
- AUTH0_AUDIENCE=https://api.watchlistnotify.com
- TMDB_API_KEY=...
- MONGODB_URI=mongodb+srv://<user>:<pass>@<cluster>/?retryWrites=true&w=majority

Optional: Static egress IP for MongoDB Atlas

- Create a Serverless VPC Connector and Cloud NAT with a reserved static IP.
- Deploy Cloud Run with that connector and set egress to "all". Allowlist the NAT IP in Atlas.

Local test

- docker buildx build --platform linux/amd64 -t watchlist-api:dev .
- docker run --rm -p 8080:8080 \
  -e PORT=8080 \
  -e MONGODB_URI='...' \
  -e JWT_SECRET='...' \
  -e TMDB_API_KEY='...' \
  watchlist-api:dev
