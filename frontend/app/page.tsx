"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useUser } from "@auth0/nextjs-auth0";
import { Button, LoadingSpinner, EmptyState } from "@/components/ui";

type WatchlistType = "movie" | "show";
type WatchlistStatus = "planned" | "watching" | "finished";

type WatchlistItem = {
  id: string;
  title: string;
  type: WatchlistType;
  status: WatchlistStatus;
  year?: number;
  tmdb_id?: number;
  added_at?: string;
  updated_at?: string;
};

type WatchlistResponse = {
  items?: WatchlistItem[];
  error?: string;
};

type AvailabilityProvider = {
  code: string;
  name: string;
  logo_path?: string;
  link?: string;
};

type AvailabilityResponse = {
  region: string;
  providers: AvailabilityProvider[];
  unmatched_user_services?: string[];
  error?: string;
};

type ServiceResponse = {
  services?: UserService[];
  error?: string;
};

type UserService = {
  code: string;
  name: string;
  active: boolean;
  added_at?: string;
};

const typeLabels: Record<WatchlistType, string> = {
  movie: "Movie",
  show: "TV",
};

export default function Home() {
  const { user, isLoading } = useUser();
  const [watchlist, setWatchlist] = useState<WatchlistItem[]>([]);
  const [watchlistError, setWatchlistError] = useState<string | null>(null);
  const [watchlistLoading, setWatchlistLoading] = useState(true);
  const [services, setServices] = useState<UserService[]>([]);
  const [servicesError, setServicesError] = useState<string | null>(null);
  const [servicesLoading, setServicesLoading] = useState(true);
  const [availabilityMap, setAvailabilityMap] = useState<
    Record<string, AvailabilityResponse | null>
  >({});
  const [availabilityLoading, setAvailabilityLoading] = useState(false);

  const isMountedRef = useRef(true);

  useEffect(() => {
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  const loadAvailability = useCallback(async (items: WatchlistItem[]) => {
    if (!items.length) {
      if (isMountedRef.current) {
        setAvailabilityMap({});
      }
      return;
    }

    const itemsToFetch = items.filter((entry) => entry.tmdb_id).slice(0, 12); // Avoid spamming the API by capping availability lookups.

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
  }, []);

  useEffect(() => {
    if (!user) {
      return;
    }

    const controller = new AbortController();
    setWatchlistLoading(true);
    setWatchlistError(null);

    (async () => {
      try {
        const response = await fetch("/api/watchlist?sort=-added_at", {
          signal: controller.signal,
        });

        const data = (await response
          .json()
          .catch(() => null)) as WatchlistResponse | null;

        if (!isMountedRef.current) {
          return;
        }

        if (!response.ok) {
          const message = data?.error || "Failed to load your watchlist.";
          setWatchlistError(message);
          setWatchlist([]);
          setAvailabilityMap({});
          return;
        }

        const fetchedItems = Array.isArray(data?.items) ? data?.items : [];
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
        if (isMountedRef.current) {
          setWatchlistLoading(false);
        }
      }
    })();

    return () => {
      controller.abort();
    };
  }, [loadAvailability, user]);

  useEffect(() => {
    if (!user) {
      return;
    }

    const controller = new AbortController();
    setServicesLoading(true);
    setServicesError(null);

    (async () => {
      try {
        const response = await fetch("/api/me/services", {
          signal: controller.signal,
        });

        const data = (await response
          .json()
          .catch(() => null)) as ServiceResponse | null;

        if (!isMountedRef.current) {
          return;
        }

        if (!response.ok) {
          const message = data?.error || "Failed to load your services.";
          setServicesError(message);
          setServices([]);
          return;
        }

        const fetchedServices = Array.isArray(data?.services)
          ? data?.services
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
        if (isMountedRef.current) {
          setServicesLoading(false);
        }
      }
    })();

    return () => {
      controller.abort();
    };
  }, [user]);

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
      return [] as {
        item: WatchlistItem;
        providers: AvailabilityProvider[];
      }[];
    }

    const matches: {
      item: WatchlistItem;
      providers: AvailabilityProvider[];
    }[] = [];

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
    () => availableForYou.slice(0, 5),
    [availableForYou]
  );

  const watchingCount = useMemo(
    () => watchlist.filter((item) => item.status === "watching").length,
    [watchlist]
  );

  const finishedCount = useMemo(
    () => watchlist.filter((item) => item.status === "finished").length,
    [watchlist]
  );

  const stats = useMemo(
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

  const formatDisplayDate = useCallback((value?: string) => {
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
  }, []);

  const availableSectionLoading =
    watchlistLoading || servicesLoading || availabilityLoading;

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

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  if (!user) {
    return (
      <div className="bg-gradient-to-br from-blue-50 to-indigo-100">
        {/* Hero Section */}
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-20">
          <div className="text-center">
            <h1 className="text-4xl md:text-6xl font-bold text-gray-900 mb-6">
              Never Miss Your
              <span className="bg-gradient-to-r from-blue-600 to-purple-600 bg-clip-text text-transparent">
                {" "}
                Favorite Shows
              </span>
            </h1>
            <p className="text-xl text-gray-600 mb-8 max-w-3xl mx-auto">
              Track your movie and TV show watchlist across all streaming
              platforms. Get notified instantly when content becomes available
              on your preferred services.
            </p>
            <div className="flex flex-col sm:flex-row gap-4 justify-center">
              <Button
                href="/auth/login"
                size="lg"
                className="px-8 py-4 text-lg"
              >
                Get Started Free
              </Button>
              <Button variant="outline" size="lg" className="px-8 py-4 text-lg">
                Learn More
              </Button>
            </div>
          </div>
        </div>

        {/* Features Section */}
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-20">
          <div className="text-center mb-16">
            <h2 className="text-3xl font-bold text-gray-900 mb-4">
              Everything You Need
            </h2>
            <p className="text-lg text-gray-600">
              Simple tools to manage your entertainment across all platforms
            </p>
          </div>

          <div className="grid md:grid-cols-3 gap-8">
            <div className="text-center p-6">
              <div className="w-16 h-16 bg-blue-100 rounded-full flex items-center justify-center mx-auto mb-4">
                <svg
                  className="w-8 h-8 text-blue-600"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
                  />
                </svg>
              </div>
              <h3 className="text-xl font-semibold text-gray-900 mb-2">
                Smart Search
              </h3>
              <p className="text-gray-600">
                Find movies and shows across all major streaming platforms in
                one place.
              </p>
            </div>

            <div className="text-center p-6">
              <div className="w-16 h-16 bg-green-100 rounded-full flex items-center justify-center mx-auto mb-4">
                <svg
                  className="w-8 h-8 text-green-600"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M15 17h5l-5 5-5-5h5v-12"
                  />
                </svg>
              </div>
              <h3 className="text-xl font-semibold text-gray-900 mb-2">
                Track Availability
              </h3>
              <p className="text-gray-600">
                Monitor when your watchlist items become available on your
                services.
              </p>
            </div>

            <div className="text-center p-6">
              <div className="w-16 h-16 bg-purple-100 rounded-full flex items-center justify-center mx-auto mb-4">
                <svg
                  className="w-8 h-8 text-purple-600"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M3 8l7.89 4.26c.3.16.67.16.98 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"
                  />
                </svg>
              </div>
              <h3 className="text-xl font-semibold text-gray-900 mb-2">
                Email Alerts
              </h3>
              <p className="text-gray-600">
                Get notified the moment your content is ready to watch.
              </p>
            </div>
          </div>
        </div>

        {/* CTA Section */}
        <div className="bg-gray-900 text-white">
          <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-16 text-center">
            <h2 className="text-3xl font-bold mb-4">
              Ready to Start Watching?
            </h2>
            <p className="text-xl text-gray-300 mb-8">
              Join thousands of users who never miss their favorite content.
            </p>
            <Button
              href="/auth/login"
              variant="primary"
              size="lg"
              className="bg-white text-gray-900 hover:bg-gray-100"
            >
              Sign Up Now
            </Button>
          </div>
        </div>
      </div>
    );
  }

  // User is logged in - show dashboard
  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-gray-900 mb-2">
          Welcome back, {user.name?.split(" ")[0]}!
        </h1>
        <p className="text-gray-600">
          Track what you are watching and see where to stream it next.
        </p>
      </div>

      {(watchlistError || servicesError) && (
        <div className="mb-6 space-y-2">
          {watchlistError && (
            <div className="rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {watchlistError}
            </div>
          )}
          {servicesError && (
            <div className="rounded-md border border-yellow-200 bg-yellow-50 px-4 py-3 text-sm text-yellow-800">
              {servicesError}
            </div>
          )}
        </div>
      )}

      <div className="grid gap-8 lg:grid-cols-4">
        <div className="lg:col-span-3 space-y-6">
          <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
            <div className="flex items-center justify-between mb-4">
              <div>
                <h2 className="text-xl font-semibold text-gray-900">
                  Quick Stats
                </h2>
                <p className="text-sm text-gray-500">
                  Snapshot of your watchlist activity
                </p>
              </div>
              <Button href="/watchlist" variant="outline" size="sm">
                View Watchlist
              </Button>
            </div>
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
              {stats.map((stat) => (
                <div
                  key={stat.label}
                  className="rounded-lg border border-gray-200 px-4 py-5 text-center"
                >
                  <div className={`text-3xl font-semibold ${stat.color} mb-2`}>
                    {stat.loading ? (
                      <LoadingSpinner size="sm" className="mx-auto" />
                    ) : (
                      stat.value
                    )}
                  </div>
                  <div className="text-sm text-gray-500">{stat.label}</div>
                </div>
              ))}
            </div>
          </div>

          <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
            <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4 mb-4">
              <div>
                <h2 className="text-xl font-semibold text-gray-900">
                  Now Streaming For You
                </h2>
                <p className="text-sm text-gray-500">
                  Titles from your watchlist available on connected services
                </p>
              </div>
              <Button href="/search" size="sm">
                Discover More
              </Button>
            </div>

            {availableSectionLoading ? (
              <div className="flex items-center justify-center py-12">
                <LoadingSpinner size="lg" />
              </div>
            ) : nowStreaming.length ? (
              <ul className="space-y-4">
                {nowStreaming.map(({ item, providers }) => (
                  <li
                    key={item.id}
                    className="flex flex-col gap-4 rounded-lg border border-gray-200 p-4 md:flex-row md:items-start md:justify-between"
                  >
                    <div>
                      <div className="flex flex-wrap items-center gap-2 mb-2">
                        <span className="rounded-full bg-blue-50 px-2 py-0.5 text-xs font-semibold uppercase tracking-wide text-blue-600">
                          {typeLabels[item.type]}
                        </span>
                        {item.year ? (
                          <span className="text-sm text-gray-500">
                            {item.year}
                          </span>
                        ) : null}
                        {item.updated_at ? (
                          <span className="text-sm text-gray-400">
                            Updated {formatDisplayDate(item.updated_at)}
                          </span>
                        ) : null}
                      </div>
                      <h3 className="text-lg font-semibold text-gray-900">
                        {item.title}
                      </h3>
                      {item.status !== "finished" && (
                        <p className="mt-1 text-sm text-gray-500">
                          Status:{" "}
                          {item.status === "watching" ? "Watching" : "On Deck"}
                        </p>
                      )}
                    </div>
                    <div className="md:min-w-[200px]">
                      <p className="text-xs uppercase text-gray-500 mb-2">
                        Watch on
                      </p>
                      <div className="flex flex-wrap gap-2">
                        {providers.slice(0, 3).map((provider) => (
                          <span
                            key={provider.code}
                            className="rounded-full bg-green-50 px-2.5 py-1 text-xs font-medium text-green-700"
                          >
                            {provider.name}
                          </span>
                        ))}
                        {providers.length > 3 && (
                          <span className="rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600">
                            +{providers.length - 3} more
                          </span>
                        )}
                      </div>
                    </div>
                  </li>
                ))}
              </ul>
            ) : (
              <EmptyState
                title="Nothing available right now"
                description="As soon as something on your watchlist hits a connected service, it will show up here."
                action={<Button href="/watchlist">Manage watchlist</Button>}
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
                      d="M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3-.895 3-2-1.343-2-3-2z"
                    />
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d="M21 12c0 4.418-4.03 8-9 8s-9-3.582-9-8 4.03-8 9-8 9 3.582 9 8z"
                    />
                  </svg>
                }
              />
            )}
          </div>
        </div>

        <div className="space-y-6">
          <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
            <h3 className="text-lg font-semibold text-gray-900 mb-4">
              Quick Actions
            </h3>
            <div className="space-y-3">
              <Button
                href="/search"
                variant="outline"
                className="w-full justify-start"
              >
                <svg
                  className="w-4 h-4 mr-2"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
                  />
                </svg>
                Search Content
              </Button>
              <Button
                href="/watchlist"
                variant="outline"
                className="w-full justify-start"
              >
                <svg
                  className="w-4 h-4 mr-2"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M9 5H7a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"
                  />
                </svg>
                View Watchlist
              </Button>
              <Button
                href="/settings"
                variant="outline"
                className="w-full justify-start"
              >
                <svg
                  className="w-4 h-4 mr-2"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"
                  />
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
                  />
                </svg>
                Settings
              </Button>
            </div>
          </div>

          <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
            <h3 className="text-lg font-semibold text-gray-900 mb-4">
              Your Services
            </h3>
            {servicesLoading ? (
              <div className="flex items-center justify-center py-10">
                <LoadingSpinner size="md" />
              </div>
            ) : sortedServices.length ? (
              <ul className="space-y-3">
                {sortedServices.map((service) => {
                  const connectedOn = formatDisplayDate(service.added_at);
                  return (
                    <li
                      key={service.code}
                      className="flex items-start justify-between gap-3 rounded-lg border border-gray-200 px-3 py-3"
                    >
                      <div>
                        <p className="font-medium text-gray-900">
                          {service.name}
                        </p>
                        <p className="text-xs text-gray-500">
                          {connectedOn
                            ? `Connected ${connectedOn}`
                            : "Not connected yet"}
                        </p>
                      </div>
                      <span
                        className={`rounded-full px-2.5 py-1 text-xs font-semibold ${
                          service.active
                            ? "bg-green-100 text-green-700"
                            : "bg-gray-100 text-gray-600"
                        }`}
                      >
                        {service.active ? "Active" : "Inactive"}
                      </span>
                    </li>
                  );
                })}
              </ul>
            ) : (
              <EmptyState
                title="No services connected"
                description="Connect your streaming services to get personalized availability alerts."
                action={
                  <Button href="/settings" size="sm">
                    Connect Services
                  </Button>
                }
              />
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
