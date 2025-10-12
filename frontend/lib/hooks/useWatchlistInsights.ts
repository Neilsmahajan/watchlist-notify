import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export type WatchlistType = "movie" | "show";
export type WatchlistStatus = "planned" | "watching" | "finished";

export type WatchlistItem = {
  id: string;
  title: string;
  type: WatchlistType;
  status: WatchlistStatus;
  year?: number;
  tmdb_id?: number;
  added_at?: string;
  updated_at?: string;
};

export type WatchlistResponse = {
  items?: WatchlistItem[];
  error?: string;
};

export type AvailabilityProvider = {
  code: string;
  name: string;
  logo_path?: string;
  link?: string;
};

export type AvailabilityResponse = {
  region: string;
  providers: AvailabilityProvider[];
  unmatched_user_services?: string[];
  error?: string;
};

export type UserService = {
  code: string;
  name: string;
  active: boolean;
  added_at?: string;
};

export type ServiceResponse = {
  services?: UserService[];
  error?: string;
};

export type StatSummary = {
  label: string;
  value: string;
  color: string;
  loading: boolean;
};

export const typeLabels: Record<WatchlistType, string> = {
  movie: "Movie",
  show: "TV",
};

export function formatDisplayDate(value?: string): string | null {
  if (!value) {
    return null;
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return null;
  }
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

type UseWatchlistInsightsOptions = {
  enabled?: boolean;
  maxStreamingItems?: number;
  availabilityLimit?: number;
};

type WatchlistMatch = {
  item: WatchlistItem;
  providers: AvailabilityProvider[];
};

type UseWatchlistInsightsResult = {
  watchlist: WatchlistItem[];
  watchlistLoading: boolean;
  watchlistError: string | null;
  services: UserService[];
  servicesLoading: boolean;
  servicesError: string | null;
  availabilityMap: Record<string, AvailabilityResponse | null>;
  availabilityLoading: boolean;
  stats: StatSummary[];
  availableSectionLoading: boolean;
  availableForYou: WatchlistMatch[];
  nowStreaming: WatchlistMatch[];
  sortedServices: UserService[];
  activeServices: UserService[];
  refresh: () => void;
  refreshWatchlist: () => Promise<void>;
  refreshServices: () => Promise<void>;
};

export function useWatchlistInsights(
  options: UseWatchlistInsightsOptions = {}
): UseWatchlistInsightsResult {
  const {
    enabled = true,
    maxStreamingItems = 5,
    availabilityLimit = 12,
  } = options;

  const [watchlist, setWatchlist] = useState<WatchlistItem[]>([]);
  const [watchlistError, setWatchlistError] = useState<string | null>(null);
  const [watchlistLoading, setWatchlistLoading] = useState(false);

  const [services, setServices] = useState<UserService[]>([]);
  const [servicesError, setServicesError] = useState<string | null>(null);
  const [servicesLoading, setServicesLoading] = useState(false);

  const [availabilityMap, setAvailabilityMap] = useState<
    Record<string, AvailabilityResponse | null>
  >({});
  const [availabilityLoading, setAvailabilityLoading] = useState(false);

  const isMountedRef = useRef(false);

  useEffect(() => {
    isMountedRef.current = true;
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  const loadAvailability = useCallback(
    async (items: WatchlistItem[]) => {
      if (!enabled) {
        return;
      }

      if (!items.length) {
        if (isMountedRef.current) {
          setAvailabilityMap({});
        }
        return;
      }

      const itemsToFetch = items
        .filter((entry) => entry.tmdb_id)
        .slice(0, availabilityLimit); // Avoid spamming the API by capping availability lookups.

      if (!itemsToFetch.length) {
        if (isMountedRef.current) {
          setAvailabilityMap((prev) => {
            const next = { ...prev };
            for (const entry of items) {
              next[entry.id] = null;
            }
            return next;
          });
        }
        return;
      }

      if (isMountedRef.current) {
        setAvailabilityLoading(true);
      }

      try {
        const results = await Promise.all(
          itemsToFetch.map(async (item) => {
            try {
              const typeParam = item.type === "show" ? "tv" : "movie";
              const response = await fetch(
                `/api/availability/${item.tmdb_id}?type=${typeParam}`
              );

              if (!response.ok) {
                return { id: item.id, data: null };
              }

              const data = (await response.json().catch(() => null)) as
                | AvailabilityResponse
                | { error?: string }
                | null;

              if (!data || typeof data !== "object" || "error" in data) {
                return { id: item.id, data: null };
              }

              return {
                id: item.id,
                data: data as AvailabilityResponse,
              };
            } catch (err) {
              console.error("Availability fetch error", err);
              return { id: item.id, data: null };
            }
          })
        );

        if (!isMountedRef.current) {
          return;
        }

        setAvailabilityMap((prev) => {
          const next: Record<string, AvailabilityResponse | null> = {
            ...prev,
          };
          for (const entry of items) {
            next[entry.id] = null;
          }
          for (const { id, data } of results) {
            next[id] = data;
          }
          return next;
        });
      } finally {
        if (isMountedRef.current) {
          setAvailabilityLoading(false);
        }
      }
    },
    [availabilityLimit, enabled]
  );

  const loadWatchlist = useCallback(
    async (options: { signal?: AbortSignal; silent?: boolean } = {}) => {
      const { signal, silent } = options;
      if (!enabled) {
        return;
      }

      if (!silent && isMountedRef.current) {
        setWatchlistLoading(true);
      }
      if (!silent && isMountedRef.current) {
        setWatchlistError(null);
      }

      try {
        const response = await fetch("/api/watchlist?sort=-added_at", {
          signal,
        });

        const data = (await response
          .json()
          .catch(() => null)) as WatchlistResponse | null;

        if (signal?.aborted || !isMountedRef.current) {
          return;
        }

        if (!response.ok) {
          const message = data?.error || "Failed to load your watchlist.";
          setWatchlistError(message);
          setWatchlist([]);
          setAvailabilityMap({});
          return;
        }

        const fetchedItems = Array.isArray(data?.items) ? data.items : [];
        setWatchlist(fetchedItems);
        void loadAvailability(fetchedItems);
      } catch (err) {
        if ((err as { name?: string }).name === "AbortError") {
          return;
        }
        console.error("Watchlist fetch error", err);
        if (isMountedRef.current) {
          setWatchlistError("Unable to load your watchlist. Please retry.");
          setWatchlist([]);
          setAvailabilityMap({});
        }
      } finally {
        if (!silent && isMountedRef.current) {
          setWatchlistLoading(false);
        }
      }
    },
    [enabled, loadAvailability]
  );

  const loadServices = useCallback(
    async (options: { signal?: AbortSignal; silent?: boolean } = {}) => {
      const { signal, silent } = options;
      if (!enabled) {
        return;
      }

      if (!silent && isMountedRef.current) {
        setServicesLoading(true);
      }
      if (!silent && isMountedRef.current) {
        setServicesError(null);
      }

      try {
        const response = await fetch("/api/me/services", { signal });

        const data = (await response
          .json()
          .catch(() => null)) as ServiceResponse | null;

        if (signal?.aborted || !isMountedRef.current) {
          return;
        }

        if (!response.ok) {
          const message = data?.error || "Failed to load your services.";
          setServicesError(message);
          setServices([]);
          return;
        }

        const fetchedServices = Array.isArray(data?.services)
          ? data.services
          : [];
        setServices(fetchedServices);
      } catch (err) {
        if ((err as { name?: string }).name === "AbortError") {
          return;
        }
        console.error("Services fetch error", err);
        if (isMountedRef.current) {
          setServicesError("Unable to load your services. Please retry.");
          setServices([]);
        }
      } finally {
        if (!silent && isMountedRef.current) {
          setServicesLoading(false);
        }
      }
    },
    [enabled]
  );

  useEffect(() => {
    if (!enabled) {
      setWatchlist([]);
      setWatchlistError(null);
      setWatchlistLoading(false);
      setAvailabilityMap({});
      setAvailabilityLoading(false);
      setServices([]);
      setServicesError(null);
      setServicesLoading(false);
      return;
    }

    const controller = new AbortController();
    void loadWatchlist({ signal: controller.signal });
    return () => controller.abort();
  }, [enabled, loadWatchlist]);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    const controller = new AbortController();
    void loadServices({ signal: controller.signal });
    return () => controller.abort();
  }, [enabled, loadServices]);

  const activeServices = useMemo(
    () => services.filter((service) => service.active),
    [services]
  );

  const activeServiceCodes = useMemo(() => {
    const codes = new Set<string>();
    for (const service of activeServices) {
      codes.add(service.code);
    }
    return codes;
  }, [activeServices]);

  const availableForYou = useMemo(() => {
    if (!watchlist.length || !activeServiceCodes.size) {
      return [] as WatchlistMatch[];
    }

    const matches: WatchlistMatch[] = [];

    for (const item of watchlist) {
      const availability = availabilityMap[item.id];
      if (!availability || !Array.isArray(availability.providers)) {
        continue;
      }
      const providers = availability.providers.filter((provider) =>
        activeServiceCodes.has(provider.code)
      );
      if (providers.length) {
        matches.push({ item, providers });
      }
    }

    return matches;
  }, [activeServiceCodes, availabilityMap, watchlist]);

  const nowStreaming = useMemo(
    () => availableForYou.slice(0, maxStreamingItems),
    [availableForYou, maxStreamingItems]
  );

  const watchingCount = useMemo(
    () => watchlist.filter((item) => item.status === "watching").length,
    [watchlist]
  );

  const finishedCount = useMemo(
    () => watchlist.filter((item) => item.status === "finished").length,
    [watchlist]
  );

  const stats = useMemo<StatSummary[]>(
    () => [
      {
        label: "Watchlist Items",
        value: String(watchlist.length),
        color: "text-blue-600",
        loading: watchlistLoading,
      },
      {
        label: "Currently Watching",
        value: String(watchingCount),
        color: "text-indigo-600",
        loading: watchlistLoading,
      },
      {
        label: "Available to Stream",
        value: String(availableForYou.length),
        color: "text-green-600",
        loading: watchlistLoading || servicesLoading || availabilityLoading,
      },
      {
        label: "Finished Titles",
        value: String(finishedCount),
        color: "text-amber-600",
        loading: watchlistLoading,
      },
      {
        label: "Services Connected",
        value: String(activeServices.length),
        color: "text-purple-600",
        loading: servicesLoading,
      },
    ],
    [
      activeServices.length,
      availableForYou.length,
      finishedCount,
      servicesLoading,
      watchlist.length,
      watchlistLoading,
      watchingCount,
      availabilityLoading,
    ]
  );

  const availableSectionLoading = useMemo(
    () => watchlistLoading || servicesLoading || availabilityLoading,
    [availabilityLoading, servicesLoading, watchlistLoading]
  );

  const sortedServices = useMemo(() => {
    if (!services.length) {
      return [] as UserService[];
    }
    return [...services].sort((a, b) => {
      if (a.active === b.active) {
        return a.name.localeCompare(b.name);
      }
      return a.active ? -1 : 1;
    });
  }, [services]);

  const refreshWatchlist = useCallback(async () => {
    await loadWatchlist();
  }, [loadWatchlist]);

  const refreshServices = useCallback(async () => {
    await loadServices();
  }, [loadServices]);

  const refresh = useCallback(() => {
    void refreshWatchlist();
    void refreshServices();
  }, [refreshServices, refreshWatchlist]);

  return {
    watchlist,
    watchlistLoading,
    watchlistError,
    services,
    servicesLoading,
    servicesError,
    availabilityMap,
    availabilityLoading,
    stats,
    availableSectionLoading,
    availableForYou,
    nowStreaming,
    sortedServices,
    activeServices,
    refresh,
    refreshWatchlist,
    refreshServices,
  };
}
