import type { Me, Service, WatchlistPage } from "@/lib/watchlist/types";

// Calls from the Next server to the Go API. Pages render from these directly
// instead of having the browser fetch through a proxy route after load.

export function backendUrl(path: string): string {
  const base = process.env.BACKEND_URL || "http://localhost:8080";
  return new URL(path, base).toString();
}

export type Loaded<T> =
  | { ok: true; data: T }
  | { ok: false; status: number; error: string };

async function load<T>(
  path: string,
  init: RequestInit,
  fallback: string,
): Promise<Loaded<T>> {
  try {
    const response = await fetch(backendUrl(path), {
      ...init,
      headers: { Accept: "application/json", ...init.headers },
      signal: AbortSignal.timeout(12_000),
    });
    const body = (await response.json().catch(() => null)) as
      | (T & { error?: string })
      | null;
    if (!response.ok || !body) {
      return {
        ok: false,
        status: response.status,
        error: (body && typeof body.error === "string" && body.error) || fallback,
      };
    }
    return { ok: true, data: body };
  } catch (error) {
    console.error(`backend ${path.split("?")[0]} failed`, error);
    return { ok: false, status: 0, error: fallback };
  }
}

const authed = (token: string): RequestInit => ({
  headers: { Authorization: `Bearer ${token}` },
  cache: "no-store",
});

export function getWatchlist(token: string, query: string) {
  return load<WatchlistPage>(
    `/api/watchlist?${query}`,
    authed(token),
    "Your watchlist could not be loaded.",
  );
}

export function getServices(token: string) {
  return load<{ services: Service[] }>(
    "/api/me/services",
    authed(token),
    "Your services could not be loaded.",
  );
}

export function getMe(token: string) {
  return load<Me>("/api/me", authed(token), "Your account could not be loaded.");
}

// The demo is public and the same for everyone, so its responses are cached
// by Next for five minutes: most visits never reach the API.
const DEMO_REVALIDATE = 300;

export function getDemoWatchlist(query: string) {
  return load<WatchlistPage>(
    `/api/demo/watchlist?${query}`,
    { next: { revalidate: DEMO_REVALIDATE } },
    "The demo could not be loaded.",
  );
}

export function getDemoServices() {
  return load<{ services: Service[] }>(
    "/api/demo/services",
    { next: { revalidate: DEMO_REVALIDATE } },
    "The demo could not be loaded.",
  );
}
