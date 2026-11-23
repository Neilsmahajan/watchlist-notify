# Watchlist Notify MCP Server

The API is also a [Model Context Protocol](https://modelcontextprotocol.io)
server. An AI client such as Claude connects to one endpoint, the user signs
in with the account they already have, and the client can then read and edit
that user's watchlist: "what can I watch tonight?", "add Dune to my list",
"I finished Severance".

```
POST https://api.watchlistnotify.com/mcp
```

It is the same Go service as the REST API, serving the same data through the
same functions. Nothing about it is a separate deployment.

## Contents

1. [Tools](#tools)
2. [Connecting a client](#connecting-a-client)
3. [How authorization works](#how-authorization-works)
4. [What the server checks](#what-the-server-checks)
5. [Server configuration](#server-configuration)
6. [Auth0 tenant setup](#auth0-tenant-setup)
7. [HTTP behaviour](#http-behaviour)
8. [Trying it by hand](#trying-it-by-hand)
9. [Where the code is](#where-the-code-is)

## Tools

| Tool                    | Scope             | What it does                                                                 |
| ----------------------- | ----------------- | ---------------------------------------------------------------------------- |
| `list_watchlist`        | `watchlist:read`  | The user's watchlist, newest first, with where each title streams for them   |
| `whats_streaming`       | `watchlist:read`  | Watchlist titles streaming on the user's own services, grouped by service    |
| `search_titles`         | `watchlist:read`  | Search TMDb for movies and shows, to find the ID that `add_to_watchlist` needs |
| `add_to_watchlist`      | `watchlist:write` | Add a title by TMDb ID. Adding it twice changes nothing                      |
| `update_watchlist_item` | `watchlist:write` | Change an item's status, its tags, or both                                   |
| `remove_from_watchlist` | `watchlist:write` | Permanently remove an item                                                   |

Every tool declares an input schema and an output schema, and answers with
structured content plus two text blocks: a one-line summary, and the same
structured content as JSON for clients that do not read structured content.
Results are deliberately small. They carry what a model needs to talk about a
watchlist (title, year, type, status, tags, the names of the services) and
leave out posters, logos, timestamps and synopses.

### `list_watchlist`

| Argument         | Type    | Notes                                                 |
| ---------------- | ------- | ----------------------------------------------------- |
| `status`         | string  | `planned`, `watching` or `finished`                   |
| `type`           | string  | `movie` or `show`                                     |
| `query`          | string  | Part of the title, case-insensitive                   |
| `streaming_only` | boolean | Only items on at least one of the user's own services |
| `limit`          | integer | 1 to 100, default 25                                  |
| `offset`         | integer | Pass `next_offset` from the previous page             |

```json
{
  "items": [
    {
      "id": "665f1c2ab7e4a1d2c3b4a596",
      "title": "The Matrix",
      "year": 1999,
      "type": "movie",
      "status": "planned",
      "tags": ["sci-fi"],
      "tmdb_id": 603,
      "streaming_on": ["Netflix"]
    }
  ],
  "total": 42,
  "offset": 0,
  "next_offset": 25,
  "region": "US"
}
```

`streaming_on` lists only services the user has told the app they subscribe
to, in the user's region. `next_offset` is absent on the last page.

### `whats_streaming`

Takes an optional `type` (`movie` or `show`). A title on two services is
listed under both; `streaming` and `not_streaming` count titles, not listings.

```json
{
  "services": [
    {
      "service": "Netflix",
      "titles": [
        { "id": "665f1c2ab7e4a1d2c3b4a596", "title": "The Matrix", "year": 1999, "type": "movie", "status": "planned" }
      ]
    }
  ],
  "streaming": 1,
  "not_streaming": 2,
  "your_services": ["Hulu", "Netflix"],
  "region": "US"
}
```

An empty `your_services` means the user has not chosen any services in the
app yet, and the summary says so, so that "nothing is streaming" is not
mistaken for an answer.

### `search_titles`

| Argument | Type    | Notes                                      |
| -------- | ------- | ------------------------------------------ |
| `query`  | string  | Required, up to 100 characters             |
| `type`   | string  | `movie` or `show`; both when left out      |
| `limit`  | integer | 1 to 20, default 10                        |

```json
{
  "results": [
    { "tmdb_id": 438631, "type": "movie", "title": "Dune", "year": 2021, "overview": "Paul Atreides leads a rebellion…" },
    { "tmdb_id": 90228, "type": "show", "title": "Dune: Prophecy", "year": 2024, "overview": "Two Harkonnen sisters…" }
  ]
}
```

With no `type`, movies and shows are searched at the same time and
interleaved, best of each first. The overview is cut to one short line: it is
there to tell two titles with the same name apart. Searches share the Redis
cache that `GET /api/search` fills.

### `add_to_watchlist`

Takes `tmdb_id` and `type` (`movie` or `show`), both from a `search_titles`
result. The server looks the title up itself, so the name and year on the item
are TMDb's, never the client's. The item starts as `planned`.

```json
{
  "item": { "id": "…", "title": "Inception", "year": 2010, "type": "movie", "status": "planned", "tags": [], "tmdb_id": 27205, "streaming_on": ["Hulu"] },
  "already_on_watchlist": false
}
```

It is idempotent: if the title is already on the watchlist, nothing is
written and the existing item comes back with `already_on_watchlist: true`.
If TMDb does not know the ID, or cannot be reached, nothing is added and the
tool reports an error.

### `update_watchlist_item`

Takes the item's `id` (from `list_watchlist`) and at least one of `status`
and `tags`. A field left out keeps its value. `tags` replaces the whole list
(at most 20 tags of 50 characters); an empty list removes them all. Returns
the item after the change.

### `remove_from_watchlist`

Takes the item's `id`. The removal is permanent and the tool is annotated as
destructive, so clients ask the user first. Returns `{"removed": true, "id": "…"}`.

### Errors

A tool that cannot do what was asked returns a result with `isError: true`
and a sentence a model can act on ("No item with that id is on the user's
watchlist."). Arguments that do not match the input schema are refused before
the tool runs. Internal failures are logged on the server and reported to the
client without their details.

An ID that belongs to another user's item gets exactly the answer an ID that
does not exist gets.

## Connecting a client

### Claude Code

```bash
claude mcp add --transport http watchlist-notify https://api.watchlistnotify.com/mcp
```

Then run `/mcp` inside Claude Code, pick `watchlist-notify` and choose
**Authenticate**. A browser window opens on the Auth0 sign-in page; after
consent the tools are available.

### claude.ai and Claude Desktop

**Settings → Connectors → Add custom connector**, with:

- **Name:** Watchlist Notify
- **URL:** `https://api.watchlistnotify.com/mcp`

Then **Connect** and sign in. If the tenant does not allow clients to
register themselves (see [client registration](#3-a-way-for-clients-to-register)),
open **Advanced settings** in the same dialog and enter the client ID of an
application created for Claude in Auth0.

Custom connectors are called from Anthropic's servers, not from the browser,
so they send no `Origin` header and are not affected by the endpoint's origin
policy.

## How authorization works

The endpoint is an OAuth 2.1 protected resource. Auth0, the tenant the web app
already signs in with, is the authorization server. The MCP server never sees
a password and never issues a token; it only verifies them.

```
Client                         API (/mcp)                      Auth0
  │  POST /mcp (no token)         │                               │
  │──────────────────────────────▶│                               │
  │  401 + WWW-Authenticate       │                               │
  │◀──────────────────────────────│                               │
  │  GET resource_metadata URL    │                               │
  │──────────────────────────────▶│                               │
  │  { resource, authorization_servers, scopes_supported }        │
  │◀──────────────────────────────│                               │
  │  GET /.well-known/oauth-authorization-server                  │
  │──────────────────────────────────────────────────────────────▶│
  │  authorize (PKCE) with resource=https://api.watchlistnotify.com/mcp
  │──────────────────────────────────────────────────────────────▶│
  │                      user signs in and consents               │
  │  code → token: access token with aud = the resource           │
  │◀──────────────────────────────────────────────────────────────│
  │  POST /mcp + Authorization: Bearer <token>                    │
  │──────────────────────────────▶│  verify, then run the tool    │
```

1. **The challenge.** A request without a token is answered with

   ```http
   HTTP/1.1 401 Unauthorized
   WWW-Authenticate: Bearer resource_metadata="https://api.watchlistnotify.com/.well-known/oauth-protected-resource/mcp", scope="watchlist:read watchlist:write"
   ```

2. **The resource metadata** ([RFC 9728](https://www.rfc-editor.org/rfc/rfc9728)),
   served at `/.well-known/oauth-protected-resource/mcp` and, for clients that
   only try the root, at `/.well-known/oauth-protected-resource`:

   ```json
   {
     "resource": "https://api.watchlistnotify.com/mcp",
     "authorization_servers": ["https://YOUR_TENANT.us.auth0.com/"],
     "scopes_supported": ["watchlist:read", "watchlist:write"],
     "bearer_methods_supported": ["header"],
     "resource_name": "Watchlist Notify"
   }
   ```

   `authorization_servers` is the issuer exactly as tokens name it, trailing
   slash included.

3. **The authorization server.** The client reads Auth0's own metadata,
   registers or identifies itself, and runs the authorization code flow with
   PKCE, passing `resource=https://api.watchlistnotify.com/mcp`
   ([RFC 8707](https://www.rfc-editor.org/rfc/rfc8707)).

4. **The token.** Auth0 issues a signed access token whose audience is the
   resource. The client sends it in the `Authorization` header of every
   request.

### Scopes

| Scope             | Allows                                                             |
| ----------------- | ------------------------------------------------------------------ |
| `watchlist:read`  | Reaching the endpoint at all, and the three read tools             |
| `watchlist:write` | `add_to_watchlist`, `update_watchlist_item`, `remove_from_watchlist` |

A user can connect a client read-only by granting only `watchlist:read`.
Calling a write tool with such a token is answered at the HTTP level, as the
MCP authorization specification asks, so that a client can send the user back
to grant more:

```http
HTTP/1.1 403 Forbidden
WWW-Authenticate: Bearer error="insufficient_scope", error_description="The add_to_watchlist tool needs the watchlist:write scope.", resource_metadata="…", scope="watchlist:read watchlist:write"
```

The tool also checks the scope itself when it runs. That second check is the
one that guards the data: the first has to read the tool's name out of the
request body before the protocol layer does, and a check that depends on two
parsers agreeing should not be the only one.

## What the server checks

On every request, before any tool runs:

| Check        | Rule                                                                                         | Otherwise |
| ------------ | -------------------------------------------------------------------------------------------- | --------- |
| Origin       | If the request has an `Origin` header, it must be the frontend's or the API's own            | 403       |
| Method       | `POST` (and `OPTIONS` for a preflight)                                                       | 405       |
| Token        | Present, in the `Authorization: Bearer` header. Cookies and query strings are never read     | 401       |
| Signature    | RS256, by a key in the tenant's JWKS (fetched from Auth0 and cached for five minutes)        | 401       |
| Issuer       | Exactly `https://<AUTH0_DOMAIN>/`                                                            | 401       |
| Expiry       | Not expired (one minute of clock skew is allowed)                                            | 401       |
| Audience     | Contains `MCP_RESOURCE_URL`                                                                  | 401       |
| Email        | `https://watchlistnotify.com/email` is present and `…/email_verified` is exactly `true`      | 401       |
| Scope        | `watchlist:read`                                                                             | 403       |
| Account      | Not the reserved demo account                                                                | 403       |
| Tool's scope | `watchlist:write` for the three tools that change data                                       | 403       |

A few of these deserve a sentence.

**Audience.** The tenant signs the REST API's tokens and the MCP tokens with
the same key. Only the audience tells them apart, so it is checked in both
directions: a token for the REST API is refused at `/mcp`, and a token for
`/mcp` is refused by the REST API. A token issued for anything else is never
accepted and never passed on.

**Verified email.** The user is found by the email in the token, the same way
the REST API finds them, and created on first use. An unverified address
could be anyone's, so a token without a verified email identifies no one.

**The demo account.** The public demo's account can never be used as a login,
through the REST API or through MCP.

**Every database call is scoped by the user the token names.** No tool takes
a user ID. Updates and removals match on the item's ID and the caller's user
ID together.

**Tokens are never logged.** The endpoint drops the query string of a request
before the access log sees it, in case a client put a token there.

If Auth0's keys cannot be fetched, the answer is `503` with `Retry-After`
rather than `401`, so that a client does not throw away a good token because
of an outage.

## Server configuration

One variable turns the MCP server on:

```bash
MCP_RESOURCE_URL=https://api.watchlistnotify.com/mcp
```

It is the public URL of the endpoint. It must be an absolute URL ending in
`/mcp`, with no query or fragment, and it must be character for character the
identifier of the Auth0 API below. The service refuses to start if it is set
but malformed, or set without `AUTH0_DOMAIN`.

When it is empty, the MCP endpoint and both metadata routes are not registered
at all: they answer `404` like any unknown path.

`AUTH0_DOMAIN`, already used by the REST API, names the authorization server.
`AUTH0_AUDIENCE` is untouched and stays the REST API's audience.

For local development, `MCP_RESOURCE_URL=http://localhost:8080/mcp`, with an
Auth0 API whose identifier is that same URL.

## Auth0 tenant setup

Four things have to exist in the tenant. The server depends on the first and
the last exactly as written; the middle two are about letting a client get a
token at all.

### 1. An API for the MCP endpoint

**Applications → APIs → Create API**

| Setting           | Value                                 |
| ----------------- | ------------------------------------- |
| Name              | Watchlist Notify MCP                  |
| Identifier        | `https://api.watchlistnotify.com/mcp` |
| Signing algorithm | RS256                                 |

On its **Permissions** tab, add:

| Permission        | Description                    |
| ----------------- | ------------------------------ |
| `watchlist:read`  | Read your watchlist            |
| `watchlist:write` | Add, change and remove titles  |

Leave **RBAC** off for this API. The scopes describe what a user lets a
client do with their own watchlist, which is for the user to decide on the
consent screen, not for an administrator to assign per user. With RBAC on,
Auth0 drops every scope the user has not been assigned as a permission.

This is a second API beside the existing one (`AUTH0_AUDIENCE`). Keeping them
separate is what makes the audience check mean something.

### 2. The resource parameter

MCP clients name the API they want with the standard `resource` parameter.
Auth0 historically used its own `audience` parameter and ignored `resource`,
in which case it would issue a token for the tenant's default audience, or an
opaque one, and the server would refuse it.

**Settings → Advanced → Resource Parameter Compatibility Profile:** turn it
on, so that `resource` selects the API the way `audience` does.

Do not work around this by making the MCP API the tenant's default audience:
that changes the tokens of every other application in the tenant.

### 3. A way for clients to register

A client needs a client ID. Either:

- **Let clients register themselves.** Turn on Dynamic Client Registration
  (**Settings → Advanced → OIDC Dynamic Application Registration**). Clients
  registered this way are third-party applications, which can only use
  connections promoted to domain level, so promote the connections users sign
  in with (for example `google-oauth2` and the database connection). This is
  what makes "paste the URL and connect" work in any MCP client. It also lets
  anyone register an application in the tenant, which is the usual trade-off
  of open registration.
- **Or create the application yourself.** Create an application for the
  client in Auth0, add the client's callback URL to its allowed callbacks,
  authorize it for the MCP API, and give its client ID to the client (Claude's
  connector dialog has a field for it under Advanced settings). Nothing is
  open to the public, and each new client needs a step from you.

### 4. The Action that adds the claims

Auth0 access tokens do not carry the user's email by default. A post-login
Action adds it under the app's namespace. The REST API already reads three of
these claims; `email_verified` is the one the MCP endpoint adds a need for.

```js
exports.onExecutePostLogin = async (event, api) => {
  const ns = "https://watchlistnotify.com";
  api.accessToken.setCustomClaim(`${ns}/email`, event.user.email);
  api.accessToken.setCustomClaim(`${ns}/email_verified`, event.user.email_verified === true);
  api.accessToken.setCustomClaim(`${ns}/name`, event.user.name);
  api.accessToken.setCustomClaim(`${ns}/picture`, event.user.picture);
};
```

What the server reads from an access token:

| Claim                                       | Required | Used for                                   |
| ------------------------------------------- | -------- | ------------------------------------------ |
| `iss`                                       | yes      | Must be `https://<AUTH0_DOMAIN>/`          |
| `aud`                                       | yes      | Must contain `MCP_RESOURCE_URL`            |
| `exp`                                       | yes      | Must be in the future                      |
| `scope`                                     | yes      | Space-separated; see [Scopes](#scopes)     |
| `https://watchlistnotify.com/email`         | yes      | Identifies the user                        |
| `https://watchlistnotify.com/email_verified`| yes      | Must be the boolean `true`                 |
| `https://watchlistnotify.com/name`          | no       | Name of an account created on first use    |
| `https://watchlistnotify.com/picture`       | no       | Picture of an account created on first use |

`email_verified` must be a JSON boolean. The string `"true"` is refused.

## HTTP behaviour

**One endpoint, one method.** The server implements the Streamable HTTP
transport without sessions. Each `POST` carries one JSON-RPC message and is
answered with one `application/json` document. `GET /mcp` and `DELETE /mcp`
answer `405 Method Not Allowed` with `Allow: POST`: there is no stream for
server-initiated messages to open and no session to end.

**Stateless.** No session ID is issued and nothing is remembered between two
requests, so any Cloud Run instance can serve any request, and each request is
authorized on its own. The service can scale to zero and back without a
client noticing.

**Not compressed, not under the REST API's CORS policy.** The REST API
compresses its responses and allows credentialed requests from the frontend's
origin only. Neither applies to the MCP routes, which are excluded from both
middlewares by path:

- The two metadata documents are public and readable from any origin
  (`Access-Control-Allow-Origin: *`), because a browser-based client has to
  read them before it has anything to authenticate with.
- `/mcp` has its own policy. A request with no `Origin` header (every
  non-browser client, including Claude) passes. A request with an `Origin`
  header must come from the frontend's origin or the API's own, and is
  otherwise refused with `403` before anything else is looked at, as the
  specification requires to stop DNS rebinding and cross-site requests.
  Allowed origins get CORS headers without `Allow-Credentials`: the endpoint
  takes a bearer token, never a cookie, so a page cannot borrow the session
  of a user who happens to be signed in to the web app.

  The allow-list is strict on purpose. A browser-based MCP tool served from
  another origin (the MCP Inspector, for instance) is refused in production;
  run it against a local server instead.

**Limits.** A request body is capped at 1 MiB (`413`). A tool call is given 20
seconds, inside the server's 30-second write timeout; a call out to TMDb gets
the same 8-second budget the REST handlers give it.

## Trying it by hand

```bash
# The challenge
curl -i -X POST https://api.watchlistnotify.com/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# The metadata
curl -s https://api.watchlistnotify.com/.well-known/oauth-protected-resource/mcp

# With a token whose audience is the MCP resource
curl -s -X POST https://api.watchlistnotify.com/mcp \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"whats_streaming","arguments":{}}}'
```

## Where the code is

| File                                 | What is in it                                                        |
| ------------------------------------ | -------------------------------------------------------------------- |
| `internal/middleware/mcp.go`         | Token verification, the 401 and 403 challenges, the origin check     |
| `internal/server/mcp.go`             | Configuration, routes, metadata, the chain of checks in front of the tools |
| `internal/server/mcp_tools.go`       | The six tools                                                        |
| `internal/authtest/`                 | A test authorization server that signs real tokens                   |
| `internal/server/mcp_test.go`        | End-to-end tests through the MCP SDK's own client                    |

The server is built on the official
[Go SDK](https://github.com/modelcontextprotocol/go-sdk).
