# Watchlist Notify – Bruno Collection

This folder contains Bruno requests for the API served by this repo. Open this folder (`docs/bruno/`) in Bruno to load the collection.

## Structure and scope

Organization (mirrors `internal/server/router.go`):

- Public (no auth)
  - Health: GET /health
  - Auth Login: GET /auth/google/login (redirects to Google)
  - Auth Callback: GET /auth/google/callback (typically handled in browser)
  - Logout: POST /auth/logout
- API (requires Bearer JWT)
  - Me
    - Me: GET /api/me
    - Update User Preferences: PATCH /api/me/preferences
  - Services
    - List Services: GET /api/me/services
    - Update Services: PATCH /api/me/services
  - Watchlist
    - Create Watchlist Item: POST /api/watchlist
    - List Watchlist Items: GET /api/watchlist
    - Update Watchlist Item: PATCH /api/watchlist/:id
    - Delete Watchlist Item: DELETE /api/watchlist/:id
  - Search
    - Search: GET /api/search
  - Availability
    - Availability: GET /api/availability/:id

Notes

- If you keep a single `requests/` folder that only contains protected endpoints, set auth at that folder level.
- If you add public endpoints, consider using two folders: `Public` (no auth) and `API` (auth inherited), so you don't have to override auth on public requests.

## Base URL and environment variables

You can run the collection without Bruno environments, but using them keeps things clean and lets you swap base URLs or tokens quickly.

Suggested variables

- base_url: http://localhost:8080
- token: your JWT from the Google login flow

Suggested files

- Commit: `environments/local.example.bru` (documented example only)
- Ignore: `environments/local.bru` (your real local values), and optionally `secrets/` or `vault.json` if you use Bruno secrets/vault

Example `environments/local.example.bru` contents (two valid styles):

1. .bru vars block style (matches the local.bru you’re using):

```
vars {
  base_url: http://localhost:8080
  token: REPLACE_WITH_YOUR_JWT
}
```

2. YAML-like style:

```
# Bruno Environment File (example)
name: local
vars:
  base_url: http://localhost:8080
  token: REPLACE_WITH_YOUR_JWT
```

Optional .gitignore additions (at repo root):

```
# Bruno local env and secrets
docs/bruno/environments/*.bru
!docs/bruno/environments/*.example.bru
docs/bruno/secrets/**
docs/bruno/vault.json
```

## Auth setup (Bearer JWT)

Where to configure auth

- Set Bearer auth at the folder level that contains protected endpoints (recommended: the `API` folder). This way, all requests inherit the Authorization header.
- Keep the collection root unauthenticated so `Public` requests work without overrides. If you prefer setting auth at the root, set individual public requests to “No Auth”.

How to set auth in Bruno

1. Right‑click the folder (e.g., `API`), choose “Auth”.
2. Select “Bearer Token”.
3. Put the value as `{{ token }}` to reference the environment variable, or paste the token directly.

Tip for path and query params

- Prefer variables for path params instead of literal `:id`, e.g. `{{base_url}}/api/availability/{{id}}`.
- For query strings, use Bruno’s Params UI or variables, e.g. `?type={{type}}&region={{region}}`.

## Getting a JWT token

1. Start the backend so it serves http://localhost:8080.
2. In your browser, visit `http://localhost:8080/auth/google/login` and complete the Google OAuth flow.
3. After redirect, the callback responds with JSON like:

   ```json
   { "message": "login success", "email": "you@example.com", "token": "<JWT>" }
   ```

4. Copy the `token` value and either:
   - Paste it into your Bruno environment variable `token`, or
   - Paste it into the folder’s Bearer auth token field.

Note

- The server also sets an `auth_token` HttpOnly cookie. For Bruno requests, use the Bearer token in the Authorization header rather than relying on cookies.
- Tokens expire. If you start getting 401s, repeat the login flow to obtain a fresh token.

## Using the requests

1. Ensure `base_url` is set to your API server (default: `http://localhost:8080`).
2. Ensure `token` is present (for protected endpoints).
3. Run requests in this order when first setting up:
   - Public → Auth Login (opens Google flow in browser; not typically executed from Bruno)
   - Copy token from callback JSON
   - API → Me (GET) to validate auth works

## Troubleshooting

- 401 Unauthorized: Your token is missing or expired—repeat the login flow to get a new token.
- 400 on callback: Ensure your Google OAuth env vars and redirect URL match the backend configuration (`BASE_URL` env is used to build the callback URL).
- CORS issues in the browser don’t affect Bruno; if you see CORS in Bruno, verify `AllowHeaders` include `Authorization` (they do in this repo).

## Mapping to source code

Routes are defined in `internal/server/router.go`. Public routes live at the root (e.g., `/health`, `/auth/...`, `/auth/logout`). Protected routes are under `/api` and use the `AuthRequired` middleware.

## Optional: Bruno CLI

Bruno also has a CLI for running requests in automation. If you use it in CI later, point it to this folder and select the right environment. Keep tokens out of CI unless you specifically provision test accounts/keys.
