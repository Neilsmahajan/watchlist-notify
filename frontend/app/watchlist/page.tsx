import { redirect } from "next/navigation";
import WatchlistClient from "./WatchlistClient";
import { auth0 } from "@/lib/auth0";
import {
  type WatchlistItem,
  type WatchlistResponse,
} from "@/lib/watchlist/types";

export const dynamic = "force-dynamic";

type BootstrapResult = {
  items: WatchlistItem[];
  listError: string | null;
};

// One backend call renders the page: every item comes back with its
// streaming availability attached.
async function bootstrapWatchlist(token: string): Promise<BootstrapResult> {
  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  let listError: string | null = null;
  let items: WatchlistItem[] = [];

  const watchlistUrl = new URL("/api/watchlist", backend);
  watchlistUrl.searchParams.set("sort", "-added_at");
  watchlistUrl.searchParams.set("limit", "0"); // Fetch all items

  try {
    const response = await fetch(watchlistUrl, {
      headers: {
        Authorization: `Bearer ${token}`,
        Accept: "application/json",
      },
      cache: "no-store",
    });

    const data = (await response.json().catch(() => null)) as
      WatchlistResponse | { error?: string } | null;

    if (!response.ok) {
      listError =
        (data &&
        typeof data === "object" &&
        "error" in data &&
        typeof data.error === "string"
          ? data.error
          : null) || "Failed to load watchlist.";
      return { items, listError };
    }

    const payload = data as WatchlistResponse | null;
    items = Array.isArray(payload?.items) ? payload.items : [];
  } catch (err) {
    console.error("Watchlist bootstrap error", err);
    listError = "Unable to load your watchlist. Please try again.";
    return { items, listError };
  }

  return { items, listError };
}

export default async function WatchlistPage() {
  const session = await auth0.getSession();
  if (!session?.user) {
    redirect("/auth/login");
  }

  const access = await auth0.getAccessToken().catch((err: unknown) => {
    console.error("Access token fetch error", err);
    return null;
  });

  if (!access?.token) {
    redirect("/auth/login");
  }

  const { items, listError } = await bootstrapWatchlist(access.token);

  return <WatchlistClient initialItems={items} initialListError={listError} />;
}
