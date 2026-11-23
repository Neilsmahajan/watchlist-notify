import { redirectToLogin } from "@/lib/auth/client";
import type { WatchlistItem, WatchlistPage, WatchlistStatus } from "./types";

// The changes a signed-in user can make to a title, as calls from the browser
// to this app's proxy routes. Each throws an Error whose message can be shown.

export async function readError(response: Response, fallback: string): Promise<string> {
  const body = (await response.json().catch(() => null)) as { error?: unknown } | null;
  return typeof body?.error === "string" && body.error ? body.error : fallback;
}

async function send(url: string, init: RequestInit, fallback: string): Promise<Response> {
  const response = await fetch(url, init);
  if (response.status === 401) {
    redirectToLogin();
    throw new Error("Your session has expired. Signing you in again.");
  }
  if (!response.ok) throw new Error(await readError(response, fallback));
  return response;
}

export async function setItemStatus(item: WatchlistItem, status: WatchlistStatus): Promise<void> {
  await send(
    `/api/watchlist/${encodeURIComponent(item.id)}`,
    {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status }),
    },
    "The status could not be changed.",
  );
}

export async function removeItem(item: WatchlistItem): Promise<void> {
  await send(
    `/api/watchlist/${encodeURIComponent(item.id)}`,
    { method: "DELETE" },
    "The title could not be removed.",
  );
}

// Asks the API to look the title up again, then reads the item back as the
// list now has it. Returns null if the item can no longer be found.
export async function recheckItem(item: WatchlistItem): Promise<WatchlistItem | null> {
  if (item.tmdb_id) {
    const type = item.type === "movie" ? "movie" : "tv";
    await send(
      `/api/availability/${item.tmdb_id}?type=${type}`,
      { headers: { Accept: "application/json" } },
      "Availability could not be checked.",
    );
  }
  const query = new URLSearchParams({ search: item.title.slice(0, 64), limit: "24" });
  const response = await send(
    `/api/watchlist?${query}`,
    { headers: { Accept: "application/json" } },
    "The title could not be reloaded.",
  );
  const page = (await response.json()) as WatchlistPage;
  return page.items?.find((candidate) => candidate.id === item.id) ?? null;
}
