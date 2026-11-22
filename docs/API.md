# Watchlist Notify API Documentation

Complete API reference for the Watchlist Notify backend service.

## Table of Contents

1. [Overview](#overview)
2. [Authentication](#authentication)
3. [Base URL & Endpoints](#base-url--endpoints)
4. [Interactive Documentation](#interactive-documentation)
5. [Rate Limiting](#rate-limiting)
6. [Error Handling](#error-handling)
7. [Pagination](#pagination)
8. [Endpoint Reference](#endpoint-reference)
9. [Request Examples](#request-examples)
10. [WebSocket Support](#websocket-support)

## Overview

The Watchlist Notify API provides RESTful endpoints for managing personal movie and TV show watchlists with streaming availability notifications.

**Current Version:** v0.9.0  
**Protocol:** HTTPS (production), HTTP (local development)  
**Format:** JSON

### Key Features

- **User Management** - Profile, preferences, and notification settings
- **Watchlist Operations** - CRUD operations for movies and TV shows
- **Service Management** - Track streaming service subscriptions
- **Content Discovery** - Search via TMDb integration
- **Availability** - Stored streaming availability, refreshed daily and embedded in the watchlist
- **Email Notifications** - Customizable digest emails

## Authentication

All protected endpoints require **Auth0 JWT Bearer token** authentication.

### How to Authenticate

1. **Obtain a Token**: Complete the Auth0 login flow (Google OAuth or username/password)
2. **Include in Requests**: Add the token to the `Authorization` header:

```http
Authorization: Bearer YOUR_JWT_TOKEN_HERE
```

### Token Format

```
Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6IjEyMzQ1In0...
```

### Token Expiration

Tokens expire after 24 hours. When you receive a `401 Unauthorized` response, obtain a new token through the Auth0 login flow.

### Public Endpoints

The following endpoints do **not** require authentication:

- `GET /health` - Service health check

## Base URL & Endpoints

### Production

```
https://api.watchlistnotify.com
```

### Local Development

```
http://localhost:8080
```

### Endpoint Groups

| Group         | Base Path               | Description                     |
| ------------- | ----------------------- | ------------------------------- |
| System        | `/`                     | Health and status               |
| Users         | `/api/me`               | User profile and preferences    |
| IMDb sync     | `/api/me/imdb`          | Link a public IMDb list         |
| Services      | `/api/me/services`      | Streaming service subscriptions |
| Watchlist     | `/api/watchlist`        | Watchlist CRUD operations       |
| Search        | `/api/search`           | Content discovery               |
| Availability  | `/api/availability`     | Streaming availability checks   |
| Notifications | `/api/me/notifications` | Email notification testing      |

## Interactive Documentation

### Swagger UI

Access the interactive API documentation at:

**Production:**

```
https://api.watchlistnotify.com/swagger/index.html
```

**Local:**

```
http://localhost:8080/swagger/index.html
```

### Features

- ✅ Try out endpoints directly in the browser
- ✅ View request/response schemas
- ✅ Copy cURL commands
- ✅ Authenticate with your JWT token

### OpenAPI Specification

Download the OpenAPI 3.0 spec:

- **JSON**: `https://api.watchlistnotify.com/swagger/doc.json`
- **YAML**: Available at `/docs/swagger.yaml` in the repository

## Rate Limiting

**Current Status:** No rate limiting enforced (v0.9.0)

**Planned for v1.0:**

- 100 requests per minute per user
- 1000 requests per hour per user
- Headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`

## Error Handling

### Standard Error Response

All errors return a consistent JSON structure:

```json
{
  "error": "Descriptive error message"
}
```

### HTTP Status Codes

| Code  | Meaning               | Common Causes                             |
| ----- | --------------------- | ----------------------------------------- |
| `200` | OK                    | Request successful                        |
| `201` | Created               | Resource created successfully             |
| `400` | Bad Request           | Invalid parameters, malformed JSON        |
| `401` | Unauthorized          | Missing or invalid JWT token              |
| `404` | Not Found             | Resource does not exist                   |
| `409` | Conflict              | Duplicate resource (e.g., watchlist item) |
| `500` | Internal Server Error | Server-side error                         |
| `502` | Bad Gateway           | Upstream service (TMDb) error             |
| `503` | Service Unavailable   | Service temporarily unavailable           |
| `504` | Gateway Timeout       | TMDb did not answer within 8 seconds      |

### Error Examples

**Invalid Request Body:**

```json
{
  "error": "invalid body"
}
```

**Unauthorized:**

```json
{
  "error": "unauthorized"
}
```

**Resource Not Found:**

```json
{
  "error": "not found"
}
```

## Pagination

List endpoints support pagination via query parameters.

### Parameters

| Parameter | Type    | Default | Max  | Description              |
| --------- | ------- | ------- | ---- | ------------------------ |
| `limit`   | integer | 20      | 1000 | Items per page (0 = all) |
| `offset`  | integer | 0       | -    | Items to skip            |

### Response Format

```json
{
  "items": [...],
  "meta": {
    "limit": 20,
    "offset": 0,
    "returned": 20,
    "next_offset": 20,
    "total": 42
  }
}
```

`total` is the number of items matching the filters. `next_offset` is present
only when there is a further page.

### Example

Request 10 items starting from item 20:

```
GET /api/watchlist?limit=10&offset=20
```

## Endpoint Reference

### System Endpoints

#### Health Check

```http
GET /health
```

Pings the database.

**Response:** `200 OK`

```json
{
  "status": "ok",
  "message": "It's healthy"
}
```

**Response when the database is unreachable:** `503 Service Unavailable`

```json
{
  "status": "unavailable",
  "message": "database unreachable"
}
```

---

### User Endpoints

#### Get Current User

```http
GET /api/me
```

Returns authenticated user's complete profile.

**Response:**

```json
{
  "id": "507f1f77bcf86cd799439011",
  "email": "user@example.com",
  "name": "John Doe",
  "picture": "https://...",
  "region": "US",
  "services": [
    {
      "code": "netflix",
      "active": true,
      "added_at": "2025-01-15T10:00:00Z"
    }
  ],
  "preferences": {
    "notify_email": "",
    "use_account_email": true,
    "marketing_consent": false,
    "digest_consent": true,
    "digest": {
      "enabled": true,
      "interval": 7,
      "interval_unit": "days",
      "last_sent_at": "2025-01-15T10:00:00Z",
      "next_scheduled_at": "2025-01-22T10:00:00Z"
    }
  },
  "imdb_sync": {
    "source_id": "ur12345678",
    "linked_at": "2026-03-01T09:00:00Z",
    "last_attempt_at": "2026-03-15T06:00:04Z",
    "last_synced_at": "2026-03-15T06:00:04Z",
    "last_status": "ok",
    "total": 242,
    "added": 1,
    "removed": 0,
    "unmatched": 2
  },
  "created_at": "2025-01-01T10:00:00Z",
  "updated_at": "2025-01-15T10:00:00Z"
}
```

`imdb_sync` is present only while the user has an IMDb list linked (see
[Link an IMDb List](#link-an-imdb-list)):

- `source_id` - the linked IMDb user ID (`ur…`, that user's Watchlist) or
  list ID (`ls…`)
- `last_attempt_at` - when the list was last fetched, successfully or not
- `last_synced_at` - when the whole list was last read; absent until then
- `last_status` - `ok`; `partial` (only part of the list was read or added so
  far; the next daily sync continues); `failed` (IMDb could not be reached);
  `not_found` (the list is gone or no longer public)
- `last_error` - a short explanation, present unless the status is `ok`
- `total` - titles on the IMDb list
- `added`, `removed`, `unmatched` - counts from the last run. `unmatched` are
  IMDb titles TMDb has no movie or show for (episodes, video games), which
  cannot be tracked.

#### Link an IMDb List

```http
PUT /api/me/imdb
```

Links the user's **public** IMDb Watchlist, or any public IMDb list, to their
watchlist. The list's titles are added before the request answers (it may
take up to about 20 seconds), and a daily job keeps the watchlist in step with
the list from then on. Linking again replaces the link.

**Request Body:**

```json
{ "source": "https://www.imdb.com/user/ur12345678/watchlist" }
```

`source` is an IMDb user ID (`ur12345678`), a list ID (`ls012345678`), or an
`imdb.com` URL containing one.

**Response:** `200 OK`

```json
{
  "imdb_sync": {
    "source_id": "ur12345678",
    "linked_at": "2026-03-15T10:00:00Z",
    "last_attempt_at": "2026-03-15T10:00:00Z",
    "last_synced_at": "2026-03-15T10:00:00Z",
    "last_status": "ok",
    "total": 242,
    "added": 230,
    "removed": 0,
    "unmatched": 2
  },
  "result": {
    "total": 242,
    "added": 230,
    "existing": 10,
    "unmatched": 2,
    "complete": true
  }
}
```

- `result.added` - titles that became new watchlist items; `existing` - titles
  the user already had, which are left exactly as they are
- `result.complete` - `false` when the list was only partly read, or some
  titles could not be looked up within the time the request has. What was
  resolved is added, `imdb_sync.last_status` is `partial`, and the daily sync
  finishes the job.

**Errors** (`{"error": "..."}`; the 4xx messages are written to be shown to
the user):

| Status | When                                                                                    |
| ------ | --------------------------------------------------------------------------------------- |
| `400`  | `source` is not an IMDb user ID, list ID or IMDb URL; or the list has over 10,000 titles |
| `404`  | IMDb has no such list, or it is not public. The message says to make it public          |
| `429`  | A link was attempted less than 60 seconds ago. `Retry-After` gives the seconds to wait  |
| `502`  | IMDb could not be reached                                                               |
| `503`  | The server has no TMDb key, so IMDb titles cannot be resolved                           |

A failed attempt leaves an existing link as it was.

**What the sync does**

- **Adds** each title on the list the user does not have yet, as a `planned`
  item with `"source": "imdb"`. Metadata comes from TMDb.
- **Removes** an item only when all of these hold: the sync added it
  (`source` is `imdb`); the user has not changed its status from `planned` or
  tagged it; and it was missing from two fetches of the **whole** list at
  least 12 hours apart. The first such fetch only sets `imdb_missing_since` on
  the item. Items added by hand or by CSV import are never removed, and a list
  that is private, unreachable, partly read or empty removes nothing.
- Linking never removes anything; only the daily sync does.

**IMDb's terms.** IMDb does not license its data for public or commercial
use. This feature is therefore strictly opt-in, reads only a list its owner
made public and linked themselves, stores nothing from IMDb but title IDs
(titles, posters and everything else come from TMDb), and fetches a linked
list at most once a day, one list at a time. Linking is that day's fetch.
There is deliberately no "sync now" endpoint.

#### Unlink the IMDb List

```http
DELETE /api/me/imdb
```

Stops the sync. Nothing is removed from the watchlist: the items the sync
added stay (still marked `"source": "imdb"`), and nothing removes them
automatically afterwards. Succeeds when nothing is linked.

**Response:** `204 No Content`

#### Update User Preferences

```http
PATCH /api/me/preferences
```

**Request Body:**

```json
{
  "notify_email": "custom@example.com",
  "use_account_email": false,
  "digest_consent": true,
  "digest_enabled": true,
  "digest_interval": 7,
  "digest_interval_unit": "days"
}
```

**Digest Interval Units:**

- `"days"` - 1 to 31
- `"weeks"` - 1 to 12
- `"months"` - 1 to 12

---

### Service Endpoints

#### List Streaming Services

```http
GET /api/me/services
```

Returns all available services with user's subscription status.

**Response:**

```json
{
  "services": [
    {
      "code": "netflix",
      "name": "Netflix",
      "access": "subscription",
      "active": true,
      "added_at": "2025-01-15T10:00:00Z"
    },
    {
      "code": "prime-video",
      "name": "Prime Video",
      "access": "subscription",
      "active": false
    }
  ]
}
```

#### Update Service Subscriptions

```http
PATCH /api/me/services
```

**Request Body (Option 1 - Add/Remove):**

```json
{
  "add": ["netflix", "max"],
  "remove": ["hulu"]
}
```

**Request Body (Option 2 - Toggle):**

```json
{
  "toggle": [
    { "code": "netflix", "active": true },
    { "code": "hulu", "active": false }
  ]
}
```

**Supported Service Codes:**

- `netflix`, `max`, `prime-video`, `apple-tv-plus`, `disney-plus`
- `hulu`, `paramount-plus`, `peacock`, `crunchyroll`, `tubi`
- See `/api/me/services` for complete list

---

### Watchlist Endpoints

#### Create Watchlist Item

```http
POST /api/watchlist
```

**Request Body:**

```json
{
  "title": "Inception",
  "type": "movie",
  "year": 2010,
  "tmdb_id": 27205,
  "status": "planned",
  "tags": ["sci-fi", "thriller"]
}
```

**Fields:**

- `title` _(required)_ - Title of the movie/show
- `type` - `"movie"` or `"show"` (default: `"movie"`)
- `year` - Release year
- `tmdb_id` - TMDb content ID (recommended for accurate matching)
- `imdb_id` - IMDb ID (alternative to tmdb_id)
- `status` - `"planned"`, `"watching"`, or `"finished"` (default: `"planned"`)
- `tags` - Custom tags for organization

**Response:** `201 Created`

#### List Watchlist Items

```http
GET /api/watchlist
```

**Query Parameters:**

- `limit` - Items per page (default: 20, max: 1000, 0 = all)
- `offset` - Pagination offset (default: 0)
- `status` - Filter by status: `planned`, `watching`, `finished`
- `type` - Filter by type: `movie`, `show`
- `search` - Case-insensitive match anywhere in the title
- `service` - Only items streaming on one of these services. Repeat the
  parameter or separate codes with commas: `service=netflix&service=hulu`,
  `service=netflix,hulu`
- `available` - `true`: only items streaming on a service the user has;
  `false`: only items that are not
- `sort` - `added_at`, `title`, `year`, `status`, `rating`, `runtime`,
  `available`; prefix `-` for descending (default `-added_at`). Items without
  a year, rating or runtime sort last in either direction.

**Examples:**

```
GET /api/watchlist?status=planned&type=movie&limit=10
GET /api/watchlist?search=inception&sort=-year
GET /api/watchlist?available=true&sort=-rating
GET /api/watchlist?service=netflix,hulu&limit=0
```

**Response:**

Each item carries its title metadata and its availability in the user's
region, read from stored data: the whole list costs two database queries
however long it is, and no upstream calls.

```json
{
  "items": [
    {
      "id": "665f1c2e8b3e4a0012345678",
      "user_id": "665f1b9a8b3e4a0012345600",
      "title": "Inception",
      "type": "movie",
      "year": 2010,
      "tmdb_id": 27205,
      "status": "planned",
      "added_at": "2026-01-04T18:22:10Z",
      "updated_at": "2026-01-04T18:22:10Z",
      "poster_path": "/poster.jpg",
      "runtime": 148,
      "genres": ["Action", "Science Fiction"],
      "rating": 8.4,
      "overview": "A thief who steals corporate secrets...",
      "availability": {
        "region": "US",
        "checked_at": "2026-03-15T06:00:12Z",
        "link": "https://www.themoviedb.org/movie/27205/watch?locale=US",
        "providers": [
          { "code": "netflix", "name": "Netflix", "logo_path": "/logo.jpg", "access": ["subscription"] }
        ],
        "elsewhere": [
          { "code": "max", "name": "HBO Max", "logo_path": "/logo.jpg", "access": ["subscription"] },
          { "name": "Apple TV Store", "logo_path": "/logo.jpg", "access": ["rent", "buy"] }
        ]
      }
    }
  ],
  "meta": {
    "limit": 20,
    "offset": 0,
    "returned": 1,
    "total": 1,
    "region": "US",
    "facets": {
      "services": { "netflix": 1, "max": 1 },
      "available": 1,
      "unavailable": 0,
      "status": { "planned": 1 }
    }
  }
}
```

- `source` - how the item got on the list: `"imdb"` (the IMDb sync), `"csv"`
  (a CSV import), or absent for items added by hand or before this field
  existed.
- `imdb_missing_since` - present on an IMDb-synced item that was missing from
  the last complete fetch of the linked list. If it is still missing at a
  later sync, at least 12 hours on, and the user has not changed its status or
  tagged it, the item is removed.
- `availability.providers` - the user's active services streaming the title
  (`subscription`, `free` or `ads` access).
- `availability.elsewhere` - every other offer: services the user does not
  have, and rent or buy. Providers that are not one of the supported services
  have no `code`.
- `availability` is `null` for an item without a TMDb ID, and for a title that
  could not be resolved yet. A title added moments ago is fetched on demand
  (bounded to about two seconds); otherwise the daily refresh fills it in.
- `meta.facets` counts items for building filter controls. `type` and `search`
  narrow every count. `status` counts ignore the `status` filter, so tabs keep
  their totals; `services`, `available` and `unavailable` respect `status` but
  ignore the `service` and `available` filters.
- The `Server-Timing` response header reports `db`, `tmdb` and `total` in
  milliseconds.

#### Update Watchlist Item

```http
PATCH /api/watchlist/{id}
```

**Path Parameters:**

- `id` - MongoDB ObjectID (hex string)

**Request Body:**

```json
{
  "status": "watching",
  "tags": ["favorite", "rewatch"]
}
```

**Updatable Fields:**

- `title`, `status`, `tags`, `year`, `tmdb_id`

#### Delete Watchlist Item

```http
DELETE /api/watchlist/{id}
```

**Response:**

```json
{
  "message": "watchlist item deleted"
}
```

#### Import Watchlist from CSV

```http
POST /api/watchlist/import
Content-Type: multipart/form-data
```

**Form Data:**

- `file` - CSV file (max 10MB, IMDb export format)

**CSV Format:**
Must include `Const` column with IMDb IDs (e.g., `tt0468569`)

An import only adds. Rows the user already has, and rows repeating an earlier
row, count as `duplicates`; new titles become `planned` items with
`"source": "csv"`. A file brings in at most 10,000 titles.

**Response:**

```json
{
  "imported": 42,
  "duplicates": 3,
  "errors": [
    {
      "row": 15,
      "reason": "no tmdb match for imdb id tt1234567"
    }
  ]
}
```

- `errors` is always an array, in file order; `row` counts the header as
  row 1.
- When TMDb is slow, the rows that could not be looked up in time are reported
  in `errors` (one entry per row up to 20, otherwise a single entry giving the
  number left). Rows that made it are stored; uploading the same file again
  continues where it stopped, at no cost for the rows already in.
- `400` with `"no rows imported"` when the file has rows and none could be
  imported.

---

### Search Endpoints

#### Search Movies and TV Shows

```http
GET /api/search
```

**Query Parameters:**

- `query` _(required)_ - Search query string
- `type` - `"movie"` or `"tv"` (default: `"movie"`)
- `page` - Page number (1-1000, default: 1)
- `include_adult` - Include adult content: `1` or `true` (default: false)
- `language` - ISO 639-1 code (default: `en-US`)
- `region` - ISO 3166-1 code (default: user's region or `US`)

**Example:**

```
GET /api/search?query=inception&type=movie&page=1
```

**Response:**

```json
{
  "results": [
    {
      "id": 27205,
      "title": "Inception",
      "type": "movie",
      "year": 2010,
      "overview": "A thief who steals corporate secrets...",
      "poster_path": "/path/to/poster.jpg",
      "vote_average": 8.4
    }
  ],
  "page": 1,
  "total_pages": 5,
  "query": "inception",
  "type": "movie",
  "include_adult": false,
  "language": "en-US",
  "region": "US"
}
```

---

### Availability Endpoints

#### Get Streaming Availability

```http
GET /api/availability/{id}
```

**Path Parameters:**

- `id` - TMDb content ID

Availability is stored and refreshed daily by the worker, so this endpoint
reads from the database. TMDb is called only for a title that is not stored
yet. `GET /api/watchlist` already embeds the same data for every item; use
this endpoint for a title that is not on the list, or to re-check one.

**Query Parameters:**

- `type` _(required)_ - `"movie"` or `"tv"`
- `region` - ISO 3166-1 code (default: user's region or `US`). A region other
  than the user's is looked up live and not stored.

**Example:**

```
GET /api/availability/27205?type=movie&region=US
```

**Response:**

```json
{
  "region": "US",
  "providers": [
    {
      "code": "netflix",
      "name": "Netflix",
      "logo_path": "/path/to/logo.jpg",
      "link": "https://www.themoviedb.org/movie/27205/watch",
      "access": ["subscription"]
    }
  ],
  "unmatched_user_services": ["hulu", "prime_video"]
}
```

`providers` lists only the user's active services; it is `[]` when none of
them has the title, or when TMDb does not know the title.

**Access Types:**

- `"subscription"` - Included with subscription
- `"free"` - Free with ads or registration
- `"ads"` - Free with ads

#### Batch Availability Check

```http
POST /api/availability/batch
```

Check availability for multiple items (max 500), from stored data. Prefer
`GET /api/watchlist`, which embeds availability for every item on the list.
Titles that cannot be resolved come back with empty `providers`.

**Request Body:**

```json
{
  "items": [
    { "id": 27205, "type": "movie" },
    { "id": 1399, "type": "tv" }
  ],
  "region": "US"
}
```

**Response:**

```json
{
  "region": "US",
  "results": {
    "movie_27205": {
      "providers": [...],
      "unmatched_user_services": [...]
    },
    "tv_1399": {
      "providers": [...],
      "unmatched_user_services": [...]
    }
  }
}
```

---

### Notification Endpoints

#### Send Test Email

```http
POST /api/me/notifications/test
```

**Request Body (Optional):**

```json
{
  "type": "digest",
  "override_email": "test@example.com"
}
```

**Fields:**

- `type` - Notification type (currently only `"digest"`)
- `override_email` - Send to specific email (must be user's account or configured notify email)

**Response:**

```json
{
  "message": "test notification sent successfully",
  "sent_to": "user@example.com",
  "type": "digest",
  "sent_at": "2025-01-15T10:00:00Z"
}
```

## Request Examples

### cURL Examples

#### Get User Profile

```bash
curl -X GET "https://api.watchlistnotify.com/api/me" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

#### Add Watchlist Item

```bash
curl -X POST "https://api.watchlistnotify.com/api/watchlist" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "The Matrix",
    "type": "movie",
    "year": 1999,
    "tmdb_id": 603,
    "status": "planned"
  }'
```

#### Search Content

```bash
curl -X GET "https://api.watchlistnotify.com/api/search?query=matrix&type=movie" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

#### Update Services

```bash
curl -X PATCH "https://api.watchlistnotify.com/api/me/services" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "add": ["netflix", "max"],
    "remove": ["hulu"]
  }'
```

### JavaScript Examples

#### Using Fetch API

```javascript
// Get user profile
const response = await fetch("https://api.watchlistnotify.com/api/me", {
  headers: {
    Authorization: `Bearer ${token}`,
  },
});
const user = await response.json();

// Add watchlist item
const response = await fetch("https://api.watchlistnotify.com/api/watchlist", {
  method: "POST",
  headers: {
    Authorization: `Bearer ${token}`,
    "Content-Type": "application/json",
  },
  body: JSON.stringify({
    title: "Inception",
    type: "movie",
    year: 2010,
    tmdb_id: 27205,
  }),
});
```

#### Using Axios

```javascript
import axios from "axios";

const api = axios.create({
  baseURL: "https://api.watchlistnotify.com",
  headers: {
    Authorization: `Bearer ${token}`,
  },
});

// Get watchlist
const { data } = await api.get("/api/watchlist", {
  params: { status: "planned", limit: 20 },
});

// Update preferences
await api.patch("/api/me/preferences", {
  digest_enabled: true,
  digest_interval: 7,
  digest_interval_unit: "days",
});
```

## WebSocket Support

**Status:** Not implemented in v0.9.0

**Planned for v1.1:**

- Real-time availability updates
- Live notification delivery
- Watchlist sync across devices

## Additional Resources

- **Swagger UI**: https://api.watchlistnotify.com/swagger/index.html
- **GitHub Repository**: https://github.com/neilsmahajan/watchlist-notify
- **Bruno API Collection**: See `docs/bruno/` for importable API requests
- **Support**: contact@watchlistnotify.com

## Changelog

### v0.9.0 (Current)

- Complete API implementation
- OpenAPI 3.0 specification
- Swagger UI documentation
- Batch availability endpoint
- CSV import functionality
- Digest email notifications

### Unreleased

- `GET /api/watchlist` embeds title metadata and availability, and adds the
  `service` and `available` filters, more sort keys, `meta.total`,
  `meta.region`, `meta.facets` and a `Server-Timing` header
- Availability is stored and refreshed daily instead of fetched per request
- IMDb sync: `PUT` and `DELETE /api/me/imdb` link and unlink a public IMDb
  list, `GET /api/me` reports it as `imdb_sync`, and watchlist items carry
  `source` and `imdb_missing_since`
- The CSV import skips rows the user already has without a TMDb lookup and
  reports rows it ran out of time for
- `GET /health` returns `503` when the database is unreachable
- CORS is limited to the frontend's origin

### Upcoming

- v1.0: Rate limiting, API versioning
- v1.1: WebSocket support, webhooks
- v1.2: Advanced filtering, sorting options
