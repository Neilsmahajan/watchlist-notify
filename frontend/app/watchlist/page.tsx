"use client";

import { useUser } from "@auth0/nextjs-auth0";
import { LoadingSpinner, EmptyState, Button } from "@/components/ui";
import { redirect } from "next/navigation";
import { useCallback, useEffect, useState } from "react";

type WatchlistType = "movie" | "show";
type WatchlistStatus = "planned" | "watching" | "finished";

type WatchlistItem = {
  id: string;
  title: string;
  type: WatchlistType;
  status: WatchlistStatus;
  year?: number;
  tmdb_id?: number;
  tags?: string[];
  added_at?: string;
  updated_at?: string;
};

type WatchlistResponse = {
  items?: WatchlistItem[];
  error?: string;
};

const statusOptions: { value: WatchlistStatus; label: string }[] = [
  { value: "planned", label: "Planned" },
  { value: "watching", label: "Watching" },
  { value: "finished", label: "Finished" },
];

const typeFilters: { value: "all" | WatchlistType; label: string }[] = [
  { value: "all", label: "All" },
  { value: "movie", label: "Movies" },
  { value: "show", label: "TV" },
];

const sortOptions = [
  { value: "-added_at", label: "Recently Added" },
  { value: "title", label: "Title A-Z" },
  { value: "-year", label: "Newest Year" },
  { value: "year", label: "Oldest Year" },
];

const typeLabels: Record<WatchlistType, string> = {
  movie: "Movie",
  show: "TV",
};

const statusLabels: Record<WatchlistStatus, string> = {
  planned: "Planned",
  watching: "Watching",
  finished: "Finished",
};

export default function Watchlist() {
  const { user, isLoading } = useUser();
  const [items, setItems] = useState<WatchlistItem[]>([]);
  const [filterType, setFilterType] = useState<"all" | WatchlistType>("all");
  const [sortOption, setSortOption] = useState("-added_at");
  const [isFetching, setIsFetching] = useState(true);
  const [listError, setListError] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [updatingId, setUpdatingId] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const loadWatchlist = useCallback(
    async (signal?: AbortSignal) => {
      setListError(null);
      setActionError(null);
      setIsFetching(true);

      try {
        const params = new URLSearchParams();
        if (filterType !== "all") {
          params.set("type", filterType);
        }
        if (sortOption) {
          params.set("sort", sortOption);
        }

        const queryString = params.toString();
        const url = queryString
          ? `/api/watchlist?${queryString}`
          : "/api/watchlist";

        const response = await fetch(url, { signal });
        const data = (await response
          .json()
          .catch(() => null)) as WatchlistResponse | null;

        if (signal?.aborted) {
          return;
        }

        if (!response.ok) {
          const message = data?.error || "Failed to load watchlist.";
          setListError(message);
          setItems([]);
          return;
        }

        const fetchedItems = Array.isArray(data?.items) ? data.items : [];
        setItems(fetchedItems);
      } catch (err) {
        if ((err as { name?: string }).name === "AbortError") {
          return;
        }
        console.error("Watchlist fetch error", err);
        setListError("Unable to load your watchlist. Please try again.");
        setItems([]);
      } finally {
        if (!signal?.aborted) {
          setIsFetching(false);
        }
      }
    },
    [filterType, sortOption]
  );

  useEffect(() => {
    const controller = new AbortController();
    void loadWatchlist(controller.signal);
    return () => controller.abort();
  }, [loadWatchlist]);

  const handleRefresh = () => {
    void loadWatchlist();
  };

  const handleStatusChange = async (
    itemId: string,
    newStatus: WatchlistStatus
  ) => {
    setUpdatingId(itemId);
    setActionError(null);
    setActionMessage(null);

    try {
      const response = await fetch(`/api/watchlist/${itemId}`, {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ status: newStatus }),
      });

      const data = (await response.json().catch(() => null)) as
        | (WatchlistItem & { error?: string })
        | { error?: string }
        | null;

      if (!response.ok) {
        const message =
          (data && "error" in data && typeof data.error === "string"
            ? data.error
            : null) || "Failed to update watchlist item.";
        setActionError(message);
        return;
      }

      if (data && typeof data === "object" && "id" in data) {
        setItems((prev) =>
          prev.map((item) =>
            item.id === itemId ? { ...item, ...(data as WatchlistItem) } : item
          )
        );
      } else {
        setItems((prev) =>
          prev.map((item) =>
            item.id === itemId ? { ...item, status: newStatus } : item
          )
        );
      }

      setActionMessage(`Updated status to ${statusLabels[newStatus]}.`);
    } catch (err) {
      console.error("Watchlist update error", err);
      setActionError("Unable to update watchlist item. Please retry.");
    } finally {
      setUpdatingId(null);
    }
  };

  const handleDelete = async (itemId: string) => {
    const item = items.find((entry) => entry.id === itemId);
    if (!item) {
      return;
    }

    const confirmed = window.confirm(
      `Remove "${item.title}" from your watchlist?`
    );
    if (!confirmed) {
      return;
    }

    setDeletingId(itemId);
    setActionError(null);
    setActionMessage(null);

    try {
      const response = await fetch(`/api/watchlist/${itemId}`, {
        method: "DELETE",
      });

      const data = (await response.json().catch(() => null)) as {
        error?: string;
      } | null;

      if (!response.ok) {
        const message = data?.error || "Failed to remove watchlist item.";
        setActionError(message);
        return;
      }

      setItems((prev) => prev.filter((entry) => entry.id !== itemId));
      setActionMessage(`Removed "${item.title}" from your watchlist.`);
    } catch (err) {
      console.error("Watchlist delete error", err);
      setActionError("Unable to remove watchlist item. Please retry.");
    } finally {
      setDeletingId(null);
    }
  };

  const formatDate = useCallback((value?: string) => {
    if (!value) {
      return null;
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return null;
    }
    return date.toLocaleDateString();
  }, []);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  if (!user) {
    redirect("/auth/login");
  }

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-gray-900 mb-2">My Watchlist</h1>
        <p className="text-gray-600">
          Track your favorite movies and shows. Update their status or remove
          items when you are done.
        </p>
      </div>

      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-8">
        <div className="flex flex-wrap items-end gap-4 mb-6">
          <div className="flex flex-col">
            <label
              className="text-sm font-medium text-gray-700"
              htmlFor="watchlist-filter-type"
            >
              Type
            </label>
            <select
              id="watchlist-filter-type"
              value={filterType}
              onChange={(event) =>
                setFilterType(event.target.value as "all" | WatchlistType)
              }
              className="mt-1 w-40 rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-transparent focus:ring-2 focus:ring-blue-500"
            >
              {typeFilters.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>

          <div className="flex flex-col">
            <label
              className="text-sm font-medium text-gray-700"
              htmlFor="watchlist-sort"
            >
              Sort by
            </label>
            <select
              id="watchlist-sort"
              value={sortOption}
              onChange={(event) => setSortOption(event.target.value)}
              className="mt-1 w-48 rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-transparent focus:ring-2 focus:ring-blue-500"
            >
              {sortOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>

          <div className="ml-auto flex gap-2">
            <Button
              variant="outline"
              type="button"
              onClick={handleRefresh}
              disabled={isFetching}
            >
              Refresh
            </Button>
            <Button href="/search">Search for Content</Button>
          </div>
        </div>

        {actionMessage && (
          <div className="mb-4 rounded-md bg-green-50 px-4 py-3 text-sm text-green-700">
            {actionMessage}
          </div>
        )}
        {actionError && (
          <div className="mb-4 rounded-md bg-red-50 px-4 py-3 text-sm text-red-700">
            {actionError}
          </div>
        )}
        {listError && items.length > 0 && (
          <div className="mb-4 rounded-md bg-yellow-50 px-4 py-3 text-sm text-yellow-800">
            {listError}
          </div>
        )}

        {(() => {
          if (isFetching && items.length === 0) {
            return (
              <div className="flex flex-col items-center gap-4 py-12 text-gray-500">
                <LoadingSpinner size="lg" />
                <p>Loading your watchlist...</p>
              </div>
            );
          }

          if (listError && items.length === 0) {
            return (
              <div className="text-center text-gray-600">
                <p className="mb-4">{listError}</p>
                <Button
                  variant="outline"
                  type="button"
                  onClick={handleRefresh}
                  disabled={isFetching}
                >
                  Try again
                </Button>
              </div>
            );
          }

          if (!isFetching && items.length === 0) {
            return (
              <EmptyState
                title="Your watchlist is empty"
                description="Start by searching for movies and TV shows you want to watch."
                action={<Button href="/search">Search for Content</Button>}
                icon={
                  <svg
                    className="w-12 h-12"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d="M9 5H7a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4"
                    />
                  </svg>
                }
              />
            );
          }

          return (
            <ul className="space-y-4">
              {items.map((item) => {
                const addedDate = formatDate(item.added_at);
                const updatedDate = formatDate(item.updated_at);
                const disabling =
                  updatingId === item.id || deletingId === item.id;

                return (
                  <li
                    key={item.id}
                    className="rounded-lg border border-gray-200 p-6 shadow-sm"
                  >
                    <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                      <div>
                        <h3 className="text-xl font-semibold text-gray-900">
                          {item.title}
                        </h3>
                        <div className="mt-2 flex flex-wrap items-center gap-3 text-sm text-gray-600">
                          <span className="inline-flex items-center rounded-full bg-blue-50 px-2 py-0.5 text-xs font-medium text-blue-700">
                            {typeLabels[item.type]}
                          </span>
                          {typeof item.year === "number" && item.year > 0 && (
                            <span>{item.year}</span>
                          )}
                          {item.tmdb_id ? (
                            <span>TMDb #{item.tmdb_id}</span>
                          ) : null}
                          {item.tags && item.tags.length > 0 ? (
                            <span>Tags: {item.tags.join(", ")}</span>
                          ) : null}
                        </div>
                      </div>
                      <Button
                        variant="outline"
                        type="button"
                        onClick={() => handleDelete(item.id)}
                        loading={deletingId === item.id}
                        disabled={deletingId === item.id}
                      >
                        Remove
                      </Button>
                    </div>

                    <div className="mt-4 grid gap-4 md:grid-cols-2 md:items-center">
                      <div>
                        <label
                          htmlFor={`watchlist-status-${item.id}`}
                          className="mb-1 block text-sm font-medium text-gray-700"
                        >
                          Status
                        </label>
                        <select
                          id={`watchlist-status-${item.id}`}
                          value={item.status}
                          onChange={(event) =>
                            handleStatusChange(
                              item.id,
                              event.target.value as WatchlistStatus
                            )
                          }
                          disabled={disabling}
                          className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-transparent focus:ring-2 focus:ring-blue-500 disabled:cursor-not-allowed disabled:bg-gray-100"
                        >
                          {statusOptions.map((option) => (
                            <option key={option.value} value={option.value}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </div>

                      <div className="text-sm text-gray-500">
                        {addedDate && <p>Added {addedDate}</p>}
                        {updatedDate && <p>Updated {updatedDate}</p>}
                      </div>
                    </div>
                  </li>
                );
              })}
            </ul>
          );
        })()}
      </div>
    </div>
  );
}
