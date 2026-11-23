import { NextResponse } from "next/server";
import { backendUrl } from "@/lib/api/backend";

// The demo's "load more" and filter requests come from the browser. They go
// through here rather than straight to the API so the browser needs no CORS
// exception, and so Next can cache each distinct answer for five minutes on
// the server. The response itself is only cacheable by the visitor's own
// browser: the auth middleware runs on every route and may attach a session
// cookie, which must never end up in a shared cache.
//
// Only the parameters the API understands are passed on, and their size is
// bounded, so the cache cannot be filled with junk variations.

const ALLOWED = [
  "status",
  "type",
  "service",
  "available",
  "search",
  "sort",
  "limit",
  "offset",
];

const CACHE = "private, max-age=60";

export async function demoProxy(req: Request, path: string): Promise<Response> {
  const incoming = new URL(req.url).searchParams;
  const params = new URLSearchParams();
  for (const name of ALLOWED) {
    const value = incoming.get(name);
    if (value && value.length <= 200) params.set(name, value);
  }
  const query = params.toString();

  try {
    const response = await fetch(backendUrl(query ? `${path}?${query}` : path), {
      headers: { Accept: "application/json" },
      next: { revalidate: 300 },
      signal: AbortSignal.timeout(12_000),
    });
    const data = await response.json().catch(() => ({}));
    return NextResponse.json(data, {
      status: response.status,
      headers: { "Cache-Control": response.ok ? CACHE : "no-store" },
    });
  } catch {
    return NextResponse.json(
      { error: "The demo could not be reached. Try again in a moment." },
      { status: 502, headers: { "Cache-Control": "no-store" } },
    );
  }
}
