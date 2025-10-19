"use client";

import Image from "next/image";
import { Button, EmptyState, LoadingSpinner } from "@/components/ui";
import {
  AVAILABILITY_ACCESS_META,
  formatDisplayDate,
  getPrimaryAccess,
  typeLabels,
  useWatchlistInsights,
} from "@/lib/hooks/useWatchlistInsights";
import type { SessionUser } from "@/lib/auth/types";

interface ProfileClientProps {
  user: SessionUser | null;
}

export default function ProfileClient({ user }: ProfileClientProps) {
  const {
    stats,
    watchlist,
    watchlistLoading,
    watchlistError,
    nowStreaming,
    availableSectionLoading,
    sortedServices,
    servicesLoading,
    servicesError,
    activeServices,
    refresh,
    refreshWatchlist,
    refreshServices,
  } = useWatchlistInsights({ enabled: Boolean(user) });

  if (!user) {
    return (
      <div className="flex min-h-[50vh] flex-col items-center justify-center gap-4 text-center">
        <h1 className="text-2xl font-semibold text-gray-900">
          You need to sign in to view your profile
        </h1>
        <p className="text-gray-600">
          Sign back in to review your account details and watchlist insights.
        </p>
        <Button href="/auth/login">Sign in</Button>
      </div>
    );
  }

  const memberSince =
    formatDisplayDate(
      typeof user.created_at === "string" ? user.created_at : undefined,
    ) ||
    new Date().toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    });

  const displayName =
    typeof user.name === "string" && user.name.trim().length
      ? user.name
      : "Your Account";
  const avatarSrc =
    typeof user.picture === "string" && user.picture.trim().length
      ? user.picture
      : "/default-avatar.svg";
  const avatarAlt = displayName || "User avatar";
  const email = typeof user.email === "string" ? user.email : "";

  const watchlistStat = stats.find(
    (entry) => entry.label === "Watchlist Items",
  );
  const streamingStat = stats.find(
    (entry) => entry.label === "Available to Stream",
  );
  const servicesStat = stats.find(
    (entry) => entry.label === "Services Connected",
  );

  const summaryStats = [
    { label: "Watchlist Items", color: "text-blue-600", stat: watchlistStat },
    { label: "Available Now", color: "text-green-600", stat: streamingStat },
    { label: "Active Services", color: "text-purple-600", stat: servicesStat },
  ];

  const isRefreshing = watchlistLoading || servicesLoading;

  return (
    <div className="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-gray-900 mb-2">Profile</h1>
        <p className="text-gray-600">
          Review your account details and personalized watchlist insights
        </p>
      </div>

      <div className="space-y-6">
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <div className="flex flex-col gap-6 md:flex-row md:items-center md:justify-between">
            <div className="flex items-center gap-4">
              <Image
                src={avatarSrc}
                alt={avatarAlt}
                className="w-24 h-24 rounded-full"
                width={96}
                height={96}
              />
              <div>
                <h2 className="text-2xl font-bold text-gray-900">
                  {displayName}
                </h2>
                {email ? <p className="text-gray-600">{email}</p> : null}
                <p className="text-sm text-gray-500">
                  Member since {memberSince}
                </p>
              </div>
            </div>
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
              <Button
                variant="outline"
                href="/settings"
                className="w-full sm:w-auto"
              >
                Manage Settings
              </Button>
              <Button
                onClick={refresh}
                loading={isRefreshing}
                className="w-full sm:w-auto"
              >
                Refresh Data
              </Button>
            </div>
          </div>

          <div className="mt-6 grid gap-4 sm:grid-cols-3">
            {summaryStats.map(({ label, color, stat }) => (
              <div key={label} className="rounded-lg bg-gray-50 px-4 py-3">
                <p className="text-sm text-gray-500">{label}</p>
                <p className={`mt-1 text-2xl font-semibold ${color}`}>
                  {stat?.loading ? (
                    <LoadingSpinner size="sm" />
                  ) : (
                    (stat?.value ?? "0")
                  )}
                </p>
              </div>
            ))}
          </div>
        </div>

        {(watchlistError || servicesError) && (
          <div className="space-y-2">
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

        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
            <div>
              <h2 className="text-xl font-semibold text-gray-900">
                Activity Snapshot
              </h2>
              <p className="text-sm text-gray-500">
                Keep tabs on your watchlist and availability trends
              </p>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={refreshWatchlist}
              loading={watchlistLoading}
              className="w-full md:w-auto"
            >
              Refresh Watchlist
            </Button>
          </div>

          <div className="mt-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
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

        <div className="grid gap-6 lg:grid-cols-3">
          <div className="lg:col-span-2 space-y-6">
            <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
              <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
                <div>
                  <h2 className="text-xl font-semibold text-gray-900">
                    Streaming Highlights
                  </h2>
                  <p className="text-sm text-gray-500">
                    What you can watch right now with your connected services
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={refreshWatchlist}
                  loading={availableSectionLoading}
                  className="w-full md:w-auto"
                >
                  Refresh Availability
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
                              Updated{" "}
                              {formatDisplayDate(item.updated_at) || "recently"}
                            </span>
                          ) : null}
                        </div>
                        <h3 className="text-lg font-semibold text-gray-900">
                          {item.title}
                        </h3>
                        {item.status !== "finished" && (
                          <p className="mt-1 text-sm text-gray-500">
                            Status:{" "}
                            {item.status === "watching"
                              ? "Watching"
                              : "On Deck"}
                          </p>
                        )}
                      </div>
                      <div className="md:min-w-[200px]">
                        <p className="text-xs uppercase text-gray-500 mb-2">
                          Watch on
                        </p>
                        <div className="flex flex-wrap gap-2">
                          {providers.slice(0, 3).map((provider) => {
                            const meta =
                              AVAILABILITY_ACCESS_META[
                                getPrimaryAccess(provider)
                              ];
                            return (
                              <span
                                key={provider.code}
                                className={`rounded-full px-2.5 py-1 text-xs font-medium border border-transparent ${meta.badgeClass}`}
                              >
                                {provider.name}
                              </span>
                            );
                          })}
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
                  title="No streaming matches yet"
                  description="Connect more services or add items to your watchlist to see streaming options here."
                  action={<Button href="/search">Browse catalog</Button>}
                />
              )}
            </div>

            <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
              <h2 className="text-xl font-semibold text-gray-900 mb-3">
                Account Notes
              </h2>
              <p className="text-sm text-gray-600">
                You currently have {watchlist?.length ?? 0} titles saved to your
                watchlist. We&apos;ll alert you as soon as something becomes
                available on a connected service. Head to settings anytime to
                update your notification preferences or manage streaming
                providers.
              </p>
            </div>
          </div>

          <div className="space-y-6">
            <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
              <div className="flex flex-col gap-4">
                <div className="flex items-center justify-between">
                  <div>
                    <h2 className="text-xl font-semibold text-gray-900">
                      Connected Services
                    </h2>
                    <p className="text-sm text-gray-500">
                      {activeServices.length} of {sortedServices.length}{" "}
                      services active
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={refreshServices}
                    loading={servicesLoading}
                  >
                    Refresh
                  </Button>
                </div>

                {servicesLoading ? (
                  <div className="flex items-center justify-center py-10">
                    <LoadingSpinner size="md" />
                  </div>
                ) : sortedServices.length ? (
                  <ul className="space-y-3">
                    {sortedServices.map((service) => {
                      const connectedOn = formatDisplayDate(service.added_at);
                      const accessMeta = service.access
                        ? AVAILABILITY_ACCESS_META[service.access]
                        : null;
                      return (
                        <li
                          key={service.code}
                          className="flex items-start justify-between gap-3 rounded-lg border border-gray-200 px-3 py-3"
                        >
                          <div>
                            <p className="font-medium text-gray-900">
                              {service.name}
                            </p>
                            {accessMeta ? (
                              <span
                                className={`mt-1 inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium border border-transparent ${accessMeta.badgeClass}`}
                              >
                                {accessMeta.label}
                              </span>
                            ) : null}
                            <p className="text-xs text-gray-500">
                              {service.active
                                ? connectedOn
                                  ? `Connected ${connectedOn}`
                                  : "Connected"
                                : "Not connected"}
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
                    title="No services connected yet"
                    description="Link your streaming providers to tailor alerts and availability."
                    action={
                      <Button href="/settings" size="sm">
                        Connect Services
                      </Button>
                    }
                  />
                )}
              </div>
            </div>

            <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
              <h2 className="text-lg font-semibold text-gray-900 mb-4">
                Quick Links
              </h2>
              <div className="space-y-3">
                <Button
                  href="/watchlist"
                  variant="outline"
                  className="w-full justify-start"
                >
                  Manage Watchlist
                </Button>
                <Button
                  href="/search"
                  variant="outline"
                  className="w-full justify-start"
                >
                  Discover Content
                </Button>
                <Button
                  href="/settings"
                  variant="outline"
                  className="w-full justify-start"
                >
                  Notification Settings
                </Button>
                <Button
                  href="/auth/logout"
                  variant="secondary"
                  className="w-full justify-start"
                >
                  Sign Out
                </Button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
