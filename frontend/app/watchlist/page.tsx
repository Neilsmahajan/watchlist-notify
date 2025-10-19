import { redirect } from "next/navigation";
import WatchlistClient from "./WatchlistClient";
import { auth0 } from "@/lib/auth0";
import {
  type AvailabilityResponse,
  type WatchlistItem,
  type WatchlistResponse,
} from "@/lib/watchlist/types";

export const dynamic = "force-dynamic";

type BootstrapResult = {
  items: WatchlistItem[];
  availability: Record<string, AvailabilityResponse | null>;
  availabilityErrors: Record<string, string>;
  listError: string | null;
};

async function bootstrapWatchlist(token: string): Promise<BootstrapResult> {
  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  const availability: Record<string, AvailabilityResponse | null> = {};
  const availabilityErrors: Record<string, string> = {};
  let listError: string | null = null;
  let items: WatchlistItem[] = [];

  const watchlistUrl = new URL("/api/watchlist", backend);
  watchlistUrl.searchParams.set("sort", "-added_at");

  try {
    const response = await fetch(watchlistUrl, {
      headers: {
        Authorization: `Bearer ${token}`,
        Accept: "application/json",
      },
      cache: "no-store",
    });

    const data = (await response.json().catch(() => null)) as
      | WatchlistResponse
      | { error?: string }
      | null;

    if (!response.ok) {
      listError =
        (data &&
        typeof data === "object" &&
        "error" in data &&
        typeof data.error === "string"
          ? data.error
          : null) || "Failed to load watchlist.";
      return { items, availability, availabilityErrors, listError };
    }

    const payload = data as WatchlistResponse | null;
    items = Array.isArray(payload?.items) ? payload.items : [];
  } catch (err) {
    console.error("Watchlist bootstrap error", err);
    listError = "Unable to load your watchlist. Please try again.";
    return { items, availability, availabilityErrors, listError };
  }

  const itemsWithTmdb = items.filter((item) => item.tmdb_id);

  await Promise.all(
    itemsWithTmdb.map(async (item) => {
      const typeParam = item.type === "show" ? "tv" : "movie";
      const url = new URL(`/api/availability/${item.tmdb_id}`, backend);
      url.searchParams.set("type", typeParam);

      try {
        const response = await fetch(url, {
          headers: {
            Authorization: `Bearer ${token}`,
            Accept: "application/json",
          },
          cache: "no-store",
        });

        const data = (await response.json().catch(() => null)) as
          | AvailabilityResponse
          | { error?: string }
          | null;

        if (!response.ok || !data || typeof data !== "object") {
          availability[item.id] = null;
          availabilityErrors[item.id] =
            data &&
            typeof data === "object" &&
            "error" in data &&
            typeof data.error === "string"
              ? data.error
              : "Failed to load availability.";
          return;
        }

        if ("error" in data && data.error) {
          availability[item.id] = null;
          availabilityErrors[item.id] = data.error;
          return;
        }

        availability[item.id] = data as AvailabilityResponse;
      } catch (err) {
        console.error(`Availability bootstrap error for ${item.id}`, err);
        availability[item.id] = null;
        availabilityErrors[item.id] =
          "Unable to load availability. Please try again.";
      }
    })
  );

  for (const item of items) {
    if (!(item.id in availability)) {
      availability[item.id] = null;
    }
  }

  return { items, availability, availabilityErrors, listError };
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

  const { items, availability, availabilityErrors, listError } =
    await bootstrapWatchlist(access.token);

  return (
    <WatchlistClient
      initialItems={items}
      initialAvailability={availability}
      initialAvailabilityErrors={availabilityErrors}
      initialListError={listError}
    />
  );
}
