#!/usr/bin/env node
// A stand-in for the Go API, for looking at the frontend without a database,
// Auth0 or TMDb credentials. It is a development tool and is not part of the
// deployed app.
//
//   node scripts/mock-api.mjs            # listens on :8787
//   BACKEND_URL=http://localhost:8787 pnpm dev
//
// It serves the demo endpoints and the authenticated ones (any or no token is
// accepted) from scripts/fixtures/titles.json: real titles and poster paths,
// with made-up availability. Filtering, sorting, paging and facets follow
// internal/watchlist/view.go. State lives in memory and resets on restart.
//
// Switches, as environment variables:
//   PORT=8787        port to listen on
//   MOCK_EMPTY=1     the signed-in account starts with no items and no services
//   MOCK_NO_DEMO=1   the demo endpoints answer 404, as before the seeder has run
//   MOCK_NO_IMDB=1   the IMDb routes answer 404, as on an API without them
//   MOCK_DELAY=300   milliseconds added to every response
//   MOCK_TONIGHT=…   "What should I watch tonight?" in one of its other states:
//                    off          the API has no Anthropic key: /api/me says so
//                                 and the endpoint answers 503
//                    limit        the daily limit is used up (429)
//                    fail         the model fails (502)
//                    slow         the model takes eight seconds
//                    two          the model returns two picks
//                    empty        nothing on the list is streaming
//                    no_services  no services are chosen
//                    Without it, picks are made (by a shuffle, with canned
//                    reasons) until the limit of 10 is used up.

import { createServer } from "node:http";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";


// A copy of an object without one of its keys.
function without(object, key) {
  const copy = { ...object };
  delete copy[key];
  return copy;
}

const port = Number(process.env.PORT || 8787);
const delay = Number(process.env.MOCK_DELAY || 0);
const fixtures = JSON.parse(
  readFileSync(
    fileURLToPath(new URL("./fixtures/titles.json", import.meta.url)),
    "utf8",
  ),
);

const CATALOG = [
  ["netflix", "Netflix", "subscription", "/rK1KljqmbvO9HQa1PBFLILWah72.png"],
  ["prime_video", "Prime Video", "subscription", "/gMZdpavHmxFNnLpMHwVxfqeux2g.png"],
  ["hulu", "Hulu", "subscription", "/44uAnmSqvA4yBOdbPWN8YgQHjWm.png"],
  ["disney_plus", "Disney+", "subscription", "/5eZ872CghnHFLB1j8grszbrx0dx.png"],
  ["max", "HBO Max", "subscription", "/skypuy7SXuugIQeYg0IglmzoKaS.png"],
  ["paramount_plus", "Paramount+", "subscription", "/4N4BMd0Mm0kHAmF7RZgL5lW3cwc.png"],
  ["peacock", "Peacock", "subscription", "/a1UIdq5BrkcAxnxcUhFsNbXnxeu.png"],
  ["apple_tv_plus", "Apple TV", "subscription", "/v9SnyvYt0U3KGgBH3Sbh3z4yODe.png"],
  ["pluto_tv", "Pluto TV", "ads", "/fN4czqaMQNLeF6sSSIjGbAWzvwK.png"],
  ["tubi", "Tubi", "ads", "/9dEuvA8wg5TSeFBZlPxSVxFdimJ.png"],
  ["roku_channel", "The Roku Channel", "ads", "/b7uhrYJtAe7b1ieJGg705vTbwrX.png"],
  ["plex", "Plex", "ads", "/blAEhvx3XX8sV0fFVFk88FFjubs.png"],
  ["mgm_plus", "MGM+", "subscription", "/q63Uzpu7JAs566vA2G23Lk7LcID.png"],
  ["crunchyroll", "Crunchyroll", "subscription", "/uFL3c4Cq8M6WoLymlC5Y8bmGytV.png"],
  ["starz", "Starz", "subscription", "/h25xjouKmiSmFiiqw0aDXbxGZo7.png"],
  ["amc_plus", "AMC+", "subscription", "/wsCUflcmL4dCkzBUaGP8cj1cTkh.png"],
  ["espn_plus", "ESPN+", "subscription", "/vred6xlUjlo9TL2NlmutsZnGHUT.png"],
  ["discovery_plus", "Discovery+", "subscription", "/tHseJEgZaUdVlLtBpOGmKPfzbQ8.png"],
].map(([code, name, access, logo_path]) => ({ code, name, access, logo_path }));
const BY_CODE = new Map(CATALOG.map((s) => [s.code, s]));

// Stores that only rent or sell: no service code, as in the real data.
const STORES = [
  { name: "Apple TV Store", logo_path: "/SPnB1qiCkYfirS2it3hZORwGVn.png" },
  { name: "Amazon Video", logo_path: "/seGSXajazLMCKGB5hnRCidtjay1.png" },
  { name: "Fandango At Home", logo_path: "/19fkcOz0xeUgCVW8tO85uOYnYK9.png" },
];

const DEMO_SERVICES = ["netflix", "max", "hulu", "disney_plus", "prime_video", "apple_tv_plus"];
const NOW = Date.now();
const HOUR = 3600_000;

function hash(text) {
  let h = 2166136261;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

const hex = (n) => n.toString(16).padStart(8, "0");
const objectId = (seed) => hex(hash(seed)) + hex(hash(seed + "b")) + hex(hash(seed + "c"));

// offers decides, per title, which services stream it and which stores sell
// it. It is arbitrary but stable, and leaves roughly a fifth of the list
// streaming nowhere, so every state of the UI has something to show.
function offers(fixture) {
  const h = hash(`${fixture.type}:${fixture.tmdb_id}`);
  const streaming = [];
  if (h % 5 !== 0) {
    const count = 1 + ((h >>> 3) % 3 === 0 ? 1 : 0) + ((h >>> 5) % 7 === 0 ? 1 : 0);
    for (let i = 0; i < count; i++) {
      // Lean towards the big services so "on my services" is the common case.
      const pool = (h >>> (7 + i * 3)) % 4 === 0 ? CATALOG : CATALOG.slice(0, 8);
      const pick = pool[(h >>> (9 + i * 4)) % pool.length];
      if (!streaming.includes(pick.code)) streaming.push(pick.code);
    }
  }
  const stores = fixture.type === "movie" || h % 3 === 0 ? STORES.slice(0, 1 + (h % 3)) : [];
  return { streaming, stores };
}

function buildAvailability(fixture, active) {
  const { streaming, stores } = offers(fixture);
  const providers = [];
  const elsewhere = [];
  for (const code of streaming) {
    const service = BY_CODE.get(code);
    const entry = { code, name: service.name, logo_path: service.logo_path, access: [service.access] };
    (active.has(code) ? providers : elsewhere).push(entry);
  }
  for (const store of stores) {
    elsewhere.push({ ...store, access: ["rent", "buy"] });
  }
  const kind = fixture.type === "movie" ? "movie" : "tv";
  return {
    region: "US",
    checked_at: new Date(NOW - 5 * HOUR).toISOString(),
    link: `https://www.themoviedb.org/${kind}/${fixture.tmdb_id}/watch?locale=US`,
    providers,
    elsewhere,
  };
}

function makeItems(userId) {
  // A fixed shuffle, so films and shows are mixed when sorted by date.
  const order = [...fixtures].sort(
    (a, b) => hash(`o${a.type}${a.tmdb_id}`) - hash(`o${b.type}${b.tmdb_id}`),
  );
  return order.map((fixture, index) => {
    const added = NOW - (index * 31 + 7 + (hash(fixture.title) % 19)) * HOUR;
    return {
      id: objectId(`${userId}:${fixture.type}:${fixture.tmdb_id}`),
      user_id: userId,
      title: fixture.title,
      type: fixture.type,
      year: fixture.year,
      tmdb_id: fixture.tmdb_id,
      imdb_id: fixture.imdb_id,
      status: fixture.status,
      added_at: new Date(added).toISOString(),
      updated_at: new Date(added).toISOString(),
      fixture,
    };
  });
}

function makeAccount({ email, name, services, empty }) {
  const id = objectId(email);
  return {
    user: {
      id,
      email,
      name,
      picture: "",
      region: "US",
      services: empty
        ? []
        : services.map((code) => ({
            code,
            active: true,
            added_at: new Date(NOW - 90 * 24 * HOUR).toISOString(),
          })),
      preferences: {
        use_account_email: true,
        marketing_consent: false,
        digest_consent: !empty,
        digest: { enabled: !empty, interval: 1, interval_unit: "weeks" },
      },
      created_at: new Date(NOW - 200 * 24 * HOUR).toISOString(),
      updated_at: new Date(NOW - 2 * 24 * HOUR).toISOString(),
    },
    items: empty ? [] : makeItems(id),
  };
}

const demo = makeAccount({ email: "demo@watchlistnotify.com", name: "Demo", services: DEMO_SERVICES });
const viewer = makeAccount({
  email: "viewer@example.com",
  name: "Sam Viewer",
  services: ["netflix", "max", "hulu", "apple_tv_plus", "tubi"],
  empty: Boolean(process.env.MOCK_EMPTY),
});

const activeCodes = (user) =>
  new Set((user.services || []).filter((s) => s.active).map((s) => s.code));

function view(item, active) {
  const { fixture, ...rest } = item;
  if (!fixture) return { ...rest, availability: null };
  const availability = buildAvailability(fixture, active);
  return {
    ...rest,
    poster_path: fixture.poster_path,
    runtime: fixture.runtime || (fixture.type === "show" ? 30 + (hash(fixture.title) % 4) * 10 : undefined),
    genres: fixture.genres,
    rating: fixture.rating,
    overview: fixture.overview,
    availability,
    services: new Set(offers(fixture).streaming),
  };
}

const isAvailable = (row) => Boolean(row.availability && row.availability.providers.length);

function sortRows(rows, key) {
  let field = key.replace(/^-/, "");
  let desc = key.startsWith("-");
  const get = {
    title: (r) => r.title.toLowerCase(),
    year: (r) => r.year || 0,
    status: (r) => r.status,
    rating: (r) => r.rating || 0,
    runtime: (r) => r.runtime || 0,
    available: (r) => (isAvailable(r) ? 0 : 1),
    added_at: (r) => r.added_at,
  };
  if (!get[field]) {
    field = "added_at";
    desc = true;
  }
  const missing = ["year", "rating", "runtime"].includes(field) ? (r) => !get[field](r) : null;
  return rows.sort((a, b) => {
    if (missing && missing(a) !== missing(b)) return missing(a) ? 1 : -1;
    const [x, y] = [get[field](a), get[field](b)];
    let c = x < y ? -1 : x > y ? 1 : 0;
    if (desc) c = -c;
    if (c) return c;
    if (a.added_at !== b.added_at) return a.added_at < b.added_at ? 1 : -1;
    return a.id < b.id ? -1 : 1;
  });
}

function list(account, query, { maxLimit, zeroMeansAll }) {
  const bad = (error) => ({ status: 400, body: { error } });
  const status = query.get("status") || "";
  const type = query.get("type") || "";
  const search = (query.get("search") || "").trim().toLowerCase();
  if (status && !["planned", "watching", "finished"].includes(status)) return bad("invalid status");
  if (type && !["movie", "show"].includes(type)) return bad("invalid type");
  const services = query.getAll("service").flatMap((v) => v.split(",")).map((v) => v.trim()).filter(Boolean);
  for (const code of services) if (!BY_CODE.has(code)) return bad(`invalid service: ${code}`);
  const availableParam = query.get("available");
  if (availableParam && !["true", "false"].includes(availableParam)) return bad("invalid available");

  let limit = 20;
  if (query.has("limit")) {
    limit = Number(query.get("limit"));
    if (!Number.isInteger(limit) || limit < 0) return bad("invalid limit");
    if (limit === 0) limit = zeroMeansAll ? Infinity : maxLimit;
    limit = Math.min(limit, maxLimit);
  }
  const offset = Number(query.get("offset") || 0);
  if (!Number.isInteger(offset) || offset < 0) return bad("invalid offset");

  const active = activeCodes(account.user);
  let rows = account.items
    .map((item) => view(item, active))
    .filter((r) => (!type || r.type === type) && (!search || r.title.toLowerCase().includes(search)));

  const facets = { services: {}, available: 0, unavailable: 0, status: {} };
  for (const r of rows) facets.status[r.status] = (facets.status[r.status] || 0) + 1;
  if (status) rows = rows.filter((r) => r.status === status);
  for (const r of rows) {
    for (const code of r.services || []) facets.services[code] = (facets.services[code] || 0) + 1;
    if (isAvailable(r)) facets.available++;
    else facets.unavailable++;
  }
  if (services.length) rows = rows.filter((r) => services.some((code) => r.services?.has(code)));
  if (availableParam) rows = rows.filter((r) => isAvailable(r) === (availableParam === "true"));

  sortRows(rows, query.get("sort") || "-added_at");
  const total = rows.length;
  const page = rows.slice(offset, offset + limit).map((row) => without(row, "services"));
  return {
    status: 200,
    body: {
      items: page,
      meta: {
        limit: Number.isFinite(limit) ? limit : total,
        offset,
        returned: page.length,
        total,
        ...(offset + page.length < total ? { next_offset: offset + page.length } : {}),
        region: "US",
        facets,
      },
    },
  };
}

function serviceList(user) {
  const active = activeCodes(user);
  return {
    services: CATALOG.map(({ code, name, access }) => ({ code, name, access, active: active.has(code) })),
  };
}

function searchResults(query, type) {
  const q = query.toLowerCase();
  const kind = type === "tv" ? "show" : "movie";
  return {
    results: fixtures
      .filter((f) => f.type === kind && f.title.toLowerCase().includes(q))
      .slice(0, 20)
      .map((f) => ({
        tmdb_id: f.tmdb_id,
        title: f.title,
        year: f.year,
        type,
        poster_url: `https://image.tmdb.org/t/p/w342${f.poster_path}`,
      }))
      // One result without a poster, to exercise the fallback tile.
      .concat(q.length > 2 ? [{ tmdb_id: 9000000 + (hash(q) % 1000), title: `${query} (no poster)`, year: 1999, type }] : []),
    page: 1,
    total_pages: 1,
  };
}

let imdbSync = null;

// --- "What should I watch tonight?" ----------------------------------------
// Follows internal/tonight: the same candidates, the same answers without a
// model call, the same limit. The model is a shuffle and a list of sentences.
const tonightMode = process.env.MOCK_TONIGHT || "";
const TONIGHT_LIMIT = 10;
let tonightUsed = 0;

const TONIGHT_REASONS = [
  "Warm, quick and funny, and it will not ask much of you tonight.",
  "Tense from the first scene, and you have been putting it off long enough.",
  "Short enough to finish before bed and hard to stop thinking about after.",
  "Easy company: you can start it late and still feel you got somewhere.",
  "The one on your list that people keep telling you about, and it earns it: sharp, strange and over before it wears out its welcome.",
  "A slow start that pays off, for a night with nowhere else to be.",
  "Bright, silly and well made.",
];

const TONIGHT_EMPTY = {
  no_services: "Choose the streaming services you have, and tonight's picks will come from what they are showing.",
  nothing_streaming: "Nothing you still want to watch is streaming on your services right now.",
  no_match: "Nothing streaming on your services fits that. Try a longer running time, or films and shows together.",
  exhausted: "That was every title that fits. Start over to see them again.",
};

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function nextUtcMidnight() {
  const now = new Date();
  return new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1));
}

function plainReason(row) {
  const names = row.availability.providers.map((p) => p.name);
  if (names.length === 0) return "On your list and streaming now.";
  if (names.length === 1) return `Streaming now on ${names[0]}.`;
  return `Streaming now on ${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}.`;
}

async function tonight(account, json) {
  const badRequest = (error) => ({ status: 400, body: { error } });
  if (!json || typeof json !== "object" || Array.isArray(json)) return badRequest("invalid body");
  const { mood = "", max_minutes: maxMinutes, type = "", exclude = [] } = json;
  if (typeof mood !== "string" || !Array.isArray(exclude)) return badRequest("invalid body");
  if ([...mood.trim()].length > 200) return badRequest("mood is too long: at most 200 characters");
  if (type !== "" && type !== "movie" && type !== "show") return badRequest('type must be "movie" or "show"');
  if (maxMinutes !== undefined && (!Number.isInteger(maxMinutes) || maxMinutes < 1 || maxMinutes > 600)) {
    return badRequest("max_minutes must be between 1 and 600");
  }
  if (exclude.length > 30) return badRequest("exclude holds at most 30 IDs");
  if (exclude.some((id) => typeof id !== "string" || !/^[0-9a-f]{24}$/.test(id))) {
    return badRequest("exclude must hold watchlist item IDs");
  }

  const left = () => Math.max(0, TONIGHT_LIMIT - tonightUsed);
  const none = (reason) => ({
    status: 200,
    body: { picks: [], remaining_today: left(), model: "", message: TONIGHT_EMPTY[reason], empty_reason: reason },
  });

  const active = activeCodes(account.user);
  if (active.size === 0 || tonightMode === "no_services") return none("no_services");
  const streaming = account.items
    .filter((item) => item.status === "planned" || item.status === "watching")
    .map((item) => view(item, active))
    .filter(isAvailable);
  if (streaming.length === 0 || tonightMode === "empty") return none("nothing_streaming");
  const fitting = streaming.filter(
    (row) => (!type || row.type === type) && (!maxMinutes || (row.runtime > 0 && row.runtime <= maxMinutes)),
  );
  if (fitting.length === 0) return none("no_match");
  const candidates = fitting.filter((row) => !exclude.includes(row.id));
  if (candidates.length === 0) return none("exhausted");

  const answer = (rows, reason, model) => ({
    status: 200,
    body: {
      picks: rows.map((row, index) => ({ item: without(row, "services"), reason: reason(row, index) })),
      remaining_today: left(),
      model,
    },
  });
  // Three or fewer: there is no choice to make, so no model and no count.
  if (candidates.length <= 3) return answer(candidates, plainReason, "");

  if (tonightMode === "limit" || tonightUsed >= TONIGHT_LIMIT) {
    const resets = nextUtcMidnight();
    const scope = tonightMode === "limit" ? "total" : "user";
    return {
      status: 429,
      headers: { "Retry-After": String(Math.max(1, Math.ceil((resets - Date.now()) / 1000))) },
      body: {
        error:
          scope === "total"
            ? `today's picks have all been used; the limit resets at ${resets.toISOString()}`
            : `you have used all ${TONIGHT_LIMIT} of today's picks; the limit resets at ${resets.toISOString()}`,
        scope,
        resets_at: resets.toISOString(),
      },
    };
  }

  // From here the call is made, and counts whatever comes of it.
  tonightUsed += 1;
  await sleep(tonightMode === "slow" ? 8000 : 900);
  if (tonightMode === "fail") {
    return { status: 502, body: { error: "the picks could not be made just now; try again" } };
  }
  const shuffled = candidates
    .map((row) => ({ row, order: Math.random() }))
    .sort((a, b) => a.order - b.order)
    .map(({ row }) => row);
  const offset = Math.floor(Math.random() * TONIGHT_REASONS.length);
  return answer(
    shuffled.slice(0, tonightMode === "two" ? 2 : 3),
    (_row, index) => TONIGHT_REASONS[(offset + index) % TONIGHT_REASONS.length],
    "mock",
  );
}

async function readBody(req) {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  const raw = Buffer.concat(chunks).toString("utf8");
  try {
    return { raw, json: raw ? JSON.parse(raw) : {} };
  } catch {
    return { raw, json: null };
  }
}

async function route(req, url) {
  const { pathname, searchParams } = url;
  const method = req.method || "GET";

  if (pathname === "/health") return { status: 200, body: { status: "ok", message: "mock" } };

  // --- public demo --------------------------------------------------------
  if (pathname.startsWith("/api/demo/")) {
    if (method !== "GET") return { status: 405, body: { error: "method not allowed" } };
    if (process.env.MOCK_NO_DEMO) return { status: 404, body: { error: "demo not available" } };
    if (pathname === "/api/demo/watchlist") {
      if ((searchParams.get("search") || "").length > 64) return { status: 400, body: { error: "search is too long" } };
      return list(demo, searchParams, { maxLimit: 60, zeroMeansAll: false });
    }
    if (pathname === "/api/demo/services") return { status: 200, body: serviceList(demo.user) };
    return { status: 404, body: { error: "not found" } };
  }

  // --- signed-in account --------------------------------------------------
  const me = viewer.user;
  const userView = () => (imdbSync ? { ...me, imdb_sync: imdbSync } : me);

  if (pathname === "/api/me" && method === "GET") {
    return { status: 200, body: { ...userView(), features: { recommendations: tonightMode !== "off" } } };
  }

  if (pathname === "/api/recommendations/tonight" && method === "POST") {
    if (tonightMode === "off") {
      return { status: 503, body: { error: "recommendations are not enabled on this server" } };
    }
    const { json } = await readBody(req);
    return tonight(viewer, json);
  }

  if (pathname === "/api/me/preferences" && method === "PATCH") {
    const { json } = await readBody(req);
    if (!json) return { status: 400, body: { error: "invalid body" } };
    const p = me.preferences;
    if ("notify_email" in json) p.notify_email = json.notify_email || undefined;
    if ("use_account_email" in json) p.use_account_email = Boolean(json.use_account_email);
    if ("marketing_consent" in json) p.marketing_consent = Boolean(json.marketing_consent);
    if ("digest_consent" in json) p.digest_consent = Boolean(json.digest_consent);
    if ("digest_enabled" in json) p.digest.enabled = Boolean(json.digest_enabled);
    if ("digest_interval" in json) p.digest.interval = Number(json.digest_interval);
    if ("digest_interval_unit" in json) p.digest.interval_unit = json.digest_interval_unit;
    return { status: 200, body: userView() };
  }

  if (pathname === "/api/me/services" && method === "GET") return { status: 200, body: serviceList(me) };
  if (pathname === "/api/me/services" && method === "PATCH") {
    const { json } = await readBody(req);
    const toggles = json?.toggle;
    if (!Array.isArray(toggles) || !toggles.length) return { status: 400, body: { error: "no changes requested" } };
    for (const { code, active } of toggles) {
      if (!BY_CODE.has(code)) return { status: 400, body: { error: `invalid service code in toggle: ${code}` } };
      const existing = me.services.find((s) => s.code === code);
      if (existing) existing.active = Boolean(active);
      else me.services.push({ code, active: Boolean(active), added_at: new Date().toISOString() });
    }
    return { status: 200, body: userView() };
  }

  if (pathname === "/api/me/notifications/test" && method === "POST") {
    return {
      status: 200,
      body: { message: "test notification sent successfully", sent_to: me.preferences.notify_email || me.email, type: "digest", sent_at: new Date().toISOString() },
    };
  }

  if (pathname === "/api/me/imdb") {
    // An API without the IMDb routes answers as Gin does for an unknown
    // path: 404 in plain text.
    if (process.env.MOCK_NO_IMDB) return { status: 404, text: "404 page not found" };
    if (method === "DELETE") {
      imdbSync = null;
      return { status: 204 };
    }
    if (method === "PUT") {
      const { json } = await readBody(req);
      const source = String(json?.source || "").trim();
      const id = source.match(/(ur|ls)\d{4,}/)?.[0];
      if (!id) return { status: 400, body: { error: "enter an IMDb user ID (ur…), a list ID (ls…) or the address of your watchlist" } };
      await new Promise((resolve) => setTimeout(resolve, 2500));
      if (id.endsWith("404")) return { status: 404, body: { error: "IMDb has no public list with that ID. Check that the list is public." } };
      if (id.endsWith("429")) return { status: 429, headers: { "Retry-After": "60" }, body: { error: "too many attempts; try again in a minute" } };
      if (id.endsWith("502")) return { status: 502, body: { error: "IMDb could not be reached; try again later" } };
      const now = new Date().toISOString();
      imdbSync = { source_id: id, linked_at: now, last_attempt_at: now, last_synced_at: now, last_status: "partial", total: 214, added: 187, removed: 0, unmatched: 3 };
      return { status: 200, body: { imdb_sync: imdbSync, result: { total: 214, added: 187, existing: 24, unmatched: 3, complete: false } } };
    }
  }

  if (pathname === "/api/watchlist" && method === "GET") {
    return list(viewer, searchParams, { maxLimit: 1000, zeroMeansAll: true });
  }

  if (pathname === "/api/watchlist" && method === "POST") {
    const { json } = await readBody(req);
    if (!json?.title || !["movie", "show"].includes(json.type)) return { status: 400, body: { error: "invalid body" } };
    if (viewer.items.some((i) => i.type === json.type && i.tmdb_id === json.tmdb_id)) {
      return { status: 409, body: { error: "item already exists in watchlist" } };
    }
    const fixture = fixtures.find((f) => f.type === json.type && f.tmdb_id === json.tmdb_id);
    const now = new Date().toISOString();
    const item = {
      id: objectId(`new:${json.type}:${json.tmdb_id}:${now}`),
      user_id: me.id,
      title: json.title,
      type: json.type,
      year: json.year,
      tmdb_id: json.tmdb_id,
      status: "planned",
      added_at: now,
      updated_at: now,
      fixture,
    };
    viewer.items.push(item);
    return { status: 201, body: without(item, "fixture") };
  }

  if (pathname === "/api/watchlist/import" && method === "POST") {
    await readBody(req);
    return { status: 200, body: { imported: 12, duplicates: 3, errors: [{ row: 7, reason: "no TMDb match for this IMDb ID" }] } };
  }

  const itemMatch = pathname.match(/^\/api\/watchlist\/([0-9a-f]{24})$/);
  if (itemMatch) {
    const index = viewer.items.findIndex((i) => i.id === itemMatch[1]);
    if (index < 0) return { status: 404, body: { error: "watchlist item not found" } };
    if (method === "DELETE") {
      viewer.items.splice(index, 1);
      return { status: 204 };
    }
    if (method === "PATCH") {
      const { json } = await readBody(req);
      if (json?.status && !["planned", "watching", "finished"].includes(json.status)) return { status: 400, body: { error: "invalid status" } };
      Object.assign(viewer.items[index], json?.status ? { status: json.status } : {}, { updated_at: new Date().toISOString() });
      return { status: 200, body: without(viewer.items[index], "fixture") };
    }
  }

  if (pathname === "/api/search" && method === "GET") {
    const query = (searchParams.get("query") || "").trim();
    if (!query) return { status: 400, body: { error: "query required" } };
    return { status: 200, body: searchResults(query, searchParams.get("type") === "tv" ? "tv" : "movie") };
  }

  const availabilityMatch = pathname.match(/^\/api\/availability\/(\d+)$/);
  if (availabilityMatch && method === "GET") {
    const kind = searchParams.get("type") === "tv" ? "show" : "movie";
    const fixture = fixtures.find((f) => f.type === kind && f.tmdb_id === Number(availabilityMatch[1]));
    const availability = fixture ? buildAvailability(fixture, activeCodes(me)) : { providers: [] };
    return { status: 200, body: { region: "US", providers: availability.providers, unmatched_user_services: [] } };
  }

  return { status: 404, body: { error: "not found" } };
}

createServer(async (req, res) => {
  const url = new URL(req.url || "/", `http://${req.headers.host || "localhost"}`);
  let result;
  try {
    result = await route(req, url);
  } catch (error) {
    result = { status: 500, body: { error: String(error) } };
  }
  if (delay) await new Promise((resolve) => setTimeout(resolve, delay));
  console.log(`${req.method} ${url.pathname}${url.search} -> ${result.status}`);
  if (result.text !== undefined) {
    res.writeHead(result.status, { "Content-Type": "text/plain; charset=utf-8" }).end(result.text);
    return;
  }
  if (result.body === undefined) {
    res.writeHead(result.status).end();
    return;
  }
  res.writeHead(result.status, { "Content-Type": "application/json; charset=utf-8", ...result.headers });
  res.end(JSON.stringify(result.body));
}).listen(port, () => {
  console.log(`mock API on http://localhost:${port}`);
});
