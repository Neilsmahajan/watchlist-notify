"use client";

import { useUser } from "@auth0/nextjs-auth0";
import { LoadingSpinner, EmptyState, Button } from "@/components/ui";
import Image from "next/image";
import { redirect } from "next/navigation";
import {
  AVAILABILITY_ACCESS_META,
  AVAILABILITY_ACCESS_ORDER,
  getPrimaryAccess,
  type AvailabilityAccess,
  type AvailabilityProvider,
  type AvailabilityResponse,
} from "@/lib/hooks/useWatchlistInsights";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
} from "react";

type WatchlistType = "movie" | "show";
type WatchlistStatus = "planned" | "watching" | "finished";

type WatchlistItem = {
  id: string;
  title: string;
  type: WatchlistType;
  status: WatchlistStatus;
  year?: number;
  tmdb_id?: number;
  imdb_id?: string;
  tags?: string[];
  added_at?: string;
  updated_at?: string;
};

type WatchlistResponse = {
  items?: WatchlistItem[];
  error?: string;
};

type WatchlistImportRowError = {
  row: number;
  reason: string;
};

type WatchlistImportResponse = {
  imported?: number;
  duplicates?: number;
  errors?: WatchlistImportRowError[];
  error?: string;
};

const TMDB_IMAGE_BASE = "https://media.themoviedb.org/t/p/original";

function retainByIds<T>(
  record: Record<string, T>,
  nextItems: WatchlistItem[],
): Record<string, T> {
  if (Object.keys(record).length === 0) {
    return record;
  }
  const whitelist = new Set(nextItems.map((entry) => entry.id));
  const next: Record<string, T> = {};
  for (const [key, value] of Object.entries(record)) {
    if (whitelist.has(key)) {
      next[key] = value;
    }
  }
  return next;
}

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
  const [availability, setAvailability] = useState<
    Record<string, AvailabilityResponse | null>
  >({});
  const [availabilityLoading, setAvailabilityLoading] = useState<
    Record<string, boolean>
  >({});
  const [availabilityError, setAvailabilityError] = useState<
    Record<string, string>
  >({});
  const [importing, setImporting] = useState(false);
  const [importResult, setImportResult] =
    useState<WatchlistImportResponse | null>(null);

  const isMountedRef = useRef(true);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const availabilityRef = useRef(availability);
  const availabilityLoadingRef = useRef(availabilityLoading);

  useEffect(() => {
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    availabilityRef.current = availability;
  }, [availability]);

  useEffect(() => {
    availabilityLoadingRef.current = availabilityLoading;
  }, [availabilityLoading]);

  const requestAvailability = useCallback(
    async (item: WatchlistItem, options?: { force?: boolean }) => {
      if (!item.tmdb_id || !isMountedRef.current) {
        return;
      }

      const currentAvailability = availabilityRef.current;
      const currentLoading = availabilityLoadingRef.current;

      if (
        !options?.force &&
        (currentAvailability[item.id] || currentLoading[item.id])
      ) {
        return;
      }

      setAvailabilityLoading((prev) => {
        const next = { ...prev, [item.id]: true };
        availabilityLoadingRef.current = next;
        return next;
      });
      setAvailabilityError((prev) => {
        if (!(item.id in prev)) {
          return prev;
        }
        const next = { ...prev };
        delete next[item.id];
        return next;
      });

      try {
        const typeParam = item.type === "show" ? "tv" : "movie";
        const response = await fetch(
          `/api/availability/${item.tmdb_id}?type=${typeParam}`,
        );

        const data = (await response.json().catch(() => null)) as
          | AvailabilityResponse
          | { error?: string }
          | null;

        if (!isMountedRef.current) {
          return;
        }

        if (!response.ok || !data || typeof data !== "object") {
          const message =
            (data && "error" in data && typeof data.error === "string"
              ? data.error
              : null) || "Failed to load availability.";
          setAvailabilityError((prev) => ({ ...prev, [item.id]: message }));
          return;
        }

        if ("error" in data && data.error) {
          setAvailabilityError((prev) => ({ ...prev, [item.id]: data.error! }));
          return;
        }

        const availabilityPayload = data as AvailabilityResponse;
        setAvailability((prev) => {
          const next = {
            ...prev,
            [item.id]: availabilityPayload,
          };
          availabilityRef.current = next;
          return next;
        });
      } catch (err) {
        console.error("Availability fetch error", err);
        if (!isMountedRef.current) {
          return;
        }
        setAvailabilityError((prev) => ({
          ...prev,
          [item.id]: "Unable to load availability. Please try again.",
        }));
      } finally {
        if (isMountedRef.current) {
          setAvailabilityLoading((prev) => {
            const next = {
              ...prev,
              [item.id]: false,
            };
            availabilityLoadingRef.current = next;
            return next;
          });
        }
      }
    },
    [],
  );

  const fetchAvailabilityForItems = useCallback(
    async (list: WatchlistItem[]) => {
      for (const entry of list) {
        if (!entry.tmdb_id) {
          continue;
        }
        void requestAvailability(entry);
      }
    },
    [requestAvailability],
  );

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
        setAvailability((prev) => {
          const next = retainByIds(prev, fetchedItems);
          availabilityRef.current = next;
          return next;
        });
        setAvailabilityLoading((prev) => {
          const next = retainByIds(prev, fetchedItems);
          availabilityLoadingRef.current = next;
          return next;
        });
        setAvailabilityError((prev) => retainByIds(prev, fetchedItems));
        void fetchAvailabilityForItems(fetchedItems);
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
    [filterType, sortOption, fetchAvailabilityForItems],
  );

  useEffect(() => {
    const controller = new AbortController();
    void loadWatchlist(controller.signal);
    return () => controller.abort();
  }, [loadWatchlist]);

  const handleRefresh = () => {
    void loadWatchlist();
  };

  const handleImportClick = () => {
    fileInputRef.current?.click();
  };

  const handleImportFile = useCallback(
    async (event: ChangeEvent<HTMLInputElement>) => {
      const file = event.target.files?.[0];
      event.target.value = "";

      if (!file) {
        return;
      }

      if (!file.name.toLowerCase().endsWith(".csv")) {
        setActionError("Please upload a CSV file.");
        return;
      }

      if (file.size > 10 * 1024 * 1024) {
        setActionError("File too large. Please upload a file under 10MB.");
        return;
      }

      setImporting(true);
      setActionError(null);
      setActionMessage(null);
      setImportResult(null);

      try {
        const formData = new FormData();
        formData.set("file", file);

        const response = await fetch("/api/watchlist/import", {
          method: "POST",
          body: formData,
        });

        const data = (await response
          .json()
          .catch(() => null)) as WatchlistImportResponse | null;

        if (!response.ok || !data) {
          const message =
            (data?.error && typeof data.error === "string"
              ? data.error
              : null) || "Failed to import watchlist. Please try again.";
          setActionError(message);
          return;
        }

        setImportResult(data);

        const importedCount = data.imported ?? 0;
        const duplicateCount = data.duplicates ?? 0;
        const summaryParts = [
          `${importedCount} item${importedCount === 1 ? "" : "s"} imported`,
        ];
        if (duplicateCount > 0) {
          summaryParts.push(
            `${duplicateCount} duplicate${
              duplicateCount === 1 ? "" : "s"
            } skipped`,
          );
        }
        if (Array.isArray(data.errors) && data.errors.length > 0) {
          summaryParts.push(
            `${data.errors.length} row${
              data.errors.length === 1 ? "" : "s"
            } had issues`,
          );
        }

        setActionMessage(summaryParts.join(" · "));
        await loadWatchlist();
      } catch (err) {
        console.error("Watchlist import error", err);
        setActionError("Unable to import watchlist. Please retry.");
      } finally {
        setImporting(false);
      }
    },
    [loadWatchlist],
  );

  const handleStatusChange = async (
    itemId: string,
    newStatus: WatchlistStatus,
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
            item.id === itemId ? { ...item, ...(data as WatchlistItem) } : item,
          ),
        );
      } else {
        setItems((prev) =>
          prev.map((item) =>
            item.id === itemId ? { ...item, status: newStatus } : item,
          ),
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
      `Remove "${item.title}" from your watchlist?`,
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

  const handleAvailabilityRefresh = useCallback(
    (item: WatchlistItem) => {
      void requestAvailability(item, { force: true });
    },
    [requestAvailability],
  );

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
            <Button
              type="button"
              onClick={handleImportClick}
              loading={importing}
              disabled={importing}
            >
              Import IMDb CSV
            </Button>
            <Button href="/search">Search for Content</Button>
          </div>
          <input
            ref={fileInputRef}
            type="file"
            accept=".csv,text/csv"
            className="hidden"
            onChange={handleImportFile}
          />
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
        {importResult && (
          <div className="mb-4 rounded-md bg-blue-50 px-4 py-3 text-sm text-blue-800">
            <p className="font-medium">
              Imported {importResult.imported ?? 0} item
              {importResult.imported === 1 ? "" : "s"} successfully.
            </p>
            {importResult.duplicates ? (
              <p>
                Skipped {importResult.duplicates} duplicate
                {importResult.duplicates === 1 ? "" : "s"}.
              </p>
            ) : null}
            {Array.isArray(importResult.errors) &&
            importResult.errors.length > 0 ? (
              <div className="mt-2">
                <p className="text-xs font-semibold uppercase text-blue-700">
                  Rows with issues
                </p>
                <ul className="mt-1 space-y-1 text-xs text-blue-700">
                  {importResult.errors.slice(0, 5).map(({ row, reason }) => (
                    <li key={`${row}-${reason}`}>
                      Row {row}: {reason}
                    </li>
                  ))}
                </ul>
                {importResult.errors.length > 5 ? (
                  <p className="mt-1 text-xs text-blue-700">
                    +{importResult.errors.length - 5} more issue
                    {importResult.errors.length - 5 === 1 ? "" : "s"}.
                  </p>
                ) : null}
              </div>
            ) : null}
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
                const availabilityData = availability[item.id];
                const availabilityIsLoading =
                  availabilityLoading[item.id] ?? false;
                const availabilityMessage = availabilityError[item.id];
                const groupedProviders: Record<
                  AvailabilityAccess,
                  AvailabilityProvider[]
                > = {
                  subscription: [],
                  free: [],
                  ads: [],
                };
                const seenByCategory: Record<
                  AvailabilityAccess,
                  Set<string>
                > = {
                  subscription: new Set(),
                  free: new Set(),
                  ads: new Set(),
                };
                if (
                  availabilityData &&
                  Array.isArray(availabilityData.providers)
                ) {
                  for (const provider of availabilityData.providers) {
                    if (!provider) {
                      continue;
                    }
                    const accessList =
                      provider.access && provider.access.length
                        ? Array.from(new Set(provider.access))
                        : [getPrimaryAccess(provider)];
                    for (const access of accessList) {
                      const set = seenByCategory[access];
                      if (!set || set.has(provider.code)) {
                        continue;
                      }
                      set.add(provider.code);
                      groupedProviders[access].push(provider);
                    }
                  }
                  for (const access of AVAILABILITY_ACCESS_ORDER) {
                    groupedProviders[access].sort((a, b) =>
                      (a.name || a.code).localeCompare(b.name || b.code),
                    );
                  }
                }
                const hasProviders = AVAILABILITY_ACCESS_ORDER.some(
                  (access) => groupedProviders[access].length > 0,
                );

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
                              event.target.value as WatchlistStatus,
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

                    <div className="mt-4 rounded-md border border-gray-100 bg-gray-50 p-4">
                      <div className="mb-2 flex flex-wrap items-center gap-2">
                        <p className="text-sm font-semibold text-gray-700">
                          Streaming availability
                          {availabilityData?.region
                            ? ` · ${availabilityData.region}`
                            : ""}
                        </p>
                        {item.tmdb_id ? (
                          <Button
                            size="sm"
                            variant="outline"
                            type="button"
                            onClick={() => handleAvailabilityRefresh(item)}
                            loading={availabilityIsLoading}
                          >
                            Check again
                          </Button>
                        ) : null}
                      </div>
                      {!item.tmdb_id ? (
                        <p className="text-sm text-gray-600">
                          Link a TMDb entry to check availability.
                        </p>
                      ) : availabilityIsLoading &&
                        !hasProviders &&
                        !availabilityMessage ? (
                        <div className="flex items-center gap-2 text-sm text-gray-500">
                          <LoadingSpinner size="sm" />
                          <span>Checking your services…</span>
                        </div>
                      ) : availabilityMessage ? (
                        <p className="text-sm text-red-600">
                          {availabilityMessage}
                        </p>
                      ) : hasProviders ? (
                        <div className="space-y-3">
                          {AVAILABILITY_ACCESS_ORDER.map((access) => {
                            const providersForAccess = groupedProviders[access];
                            if (!providersForAccess.length) {
                              return null;
                            }
                            const meta = AVAILABILITY_ACCESS_META[access];
                            return (
                              <div
                                key={`${item.id}-${access}`}
                                className="space-y-2"
                              >
                                <p className="text-xs font-semibold uppercase text-gray-500">
                                  {meta.label}
                                </p>
                                <div className="flex flex-wrap items-center gap-3">
                                  {providersForAccess.map((provider) => {
                                    const logoSrc = provider.logo_path
                                      ? `${TMDB_IMAGE_BASE}${provider.logo_path}`
                                      : null;
                                    return (
                                      <a
                                        key={`${item.id}-${access}-${provider.code}`}
                                        href={provider.link ?? undefined}
                                        target={
                                          provider.link ? "_blank" : undefined
                                        }
                                        rel={
                                          provider.link
                                            ? "noopener noreferrer"
                                            : undefined
                                        }
                                        className={`flex items-center gap-2 rounded-full px-3 py-1 text-sm font-medium shadow-sm transition-colors ${meta.chipClass}`}
                                      >
                                        {logoSrc ? (
                                          <Image
                                            src={logoSrc}
                                            alt={provider.name}
                                            width={28}
                                            height={28}
                                            className="h-7 w-7 rounded"
                                          />
                                        ) : null}
                                        <span>{provider.name}</span>
                                      </a>
                                    );
                                  })}
                                </div>
                              </div>
                            );
                          })}
                          {availabilityData?.unmatched_user_services &&
                          availabilityData.unmatched_user_services.length ? (
                            <p className="text-xs text-gray-500">
                              Not seeing:{" "}
                              {availabilityData.unmatched_user_services.join(
                                ", ",
                              )}
                            </p>
                          ) : null}
                        </div>
                      ) : availabilityData ? (
                        <div className="space-y-2 text-sm text-gray-600">
                          <p>
                            Not currently available on your connected services.
                          </p>
                          {availabilityData.unmatched_user_services &&
                          availabilityData.unmatched_user_services.length ? (
                            <p className="text-xs text-gray-500">
                              Still missing:{" "}
                              {availabilityData.unmatched_user_services.join(
                                ", ",
                              )}
                            </p>
                          ) : null}
                        </div>
                      ) : (
                        <p className="text-sm text-gray-600">
                          Availability will appear once checked.
                        </p>
                      )}
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
