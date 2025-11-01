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
- **Availability Checking** - Real-time and batch availability queries
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
    "returned": 15,
    "next_offset": 20,
    "total": 42
  }
}
```

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

Returns database connection status and service health.

**Response:**

```json
{
  "status": "ok",
  "message": "It's healthy"
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
  "created_at": "2025-01-01T10:00:00Z",
  "updated_at": "2025-01-15T10:00:00Z"
}
```

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
- `search` - Search in title
- `sort` - Sort field: `title`, `year`, `added_at`, `-added_at` (prefix `-` for descending)
- `with_count` - Include total count: `1` or `true`

**Examples:**

```
GET /api/watchlist?status=planned&type=movie&limit=10
GET /api/watchlist?search=inception&sort=-year
GET /api/watchlist?with_count=1
```

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

**Query Parameters:**

- `type` _(required)_ - `"movie"` or `"tv"`
- `region` - ISO 3166-1 code (default: user's region or `US`)

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
  "unmatched_user_services": ["hulu", "prime-video"]
}
```

**Access Types:**

- `"subscription"` - Included with subscription
- `"free"` - Free with ads or registration
- `"ads"` - Free with ads

#### Batch Availability Check

```http
POST /api/availability/batch
```

Check availability for multiple items (max 500).

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

### Upcoming

- v1.0: Rate limiting, API versioning
- v1.1: WebSocket support, webhooks
- v1.2: Advanced filtering, sorting options
