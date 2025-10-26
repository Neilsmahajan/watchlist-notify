"use client";

import { useState, useEffect } from "react";
import { Button, EmptyState, LoadingSpinner } from "@/components/ui";
import { useWatchlistInsights } from "@/lib/hooks/useWatchlistInsights";
import { SessionUser } from "@/lib/auth/types";
import {
  formatDisplayDate,
  AVAILABILITY_ACCESS_META,
  typeLabels,
  getPrimaryAccess,
} from "@/lib/hooks/useWatchlistInsights";

interface HomeDashboardClientProps {
  user: SessionUser | null;
}

export default function HomeDashboardClient({
  user,
}: HomeDashboardClientProps) {
  const {
    stats,
    availableSectionLoading,
    availableForYou,
    watchlistError,
    servicesError,
    sortedServices,
    servicesLoading,
  } = useWatchlistInsights({ enabled: Boolean(user) });

  const [currentPage, setCurrentPage] = useState(1);
  const [itemsPerPage, setItemsPerPage] = useState(10);

  // Pagination logic
  const totalPages = Math.ceil(availableForYou.length / itemsPerPage);
  const startIndex = (currentPage - 1) * itemsPerPage;
  const endIndex = startIndex + itemsPerPage;
  const paginatedItems = availableForYou.slice(startIndex, endIndex);

  // Reset to page 1 when available items change
  useEffect(() => {
    setCurrentPage(1);
  }, [availableForYou.length]);

  const handlePageChange = (newPage: number) => {
    setCurrentPage(newPage);
    // Scroll to the "Now Streaming" section
    document.getElementById("now-streaming")?.scrollIntoView({
      behavior: "smooth",
      block: "start",
    });
  };

  const handleItemsPerPageChange = (newItemsPerPage: number) => {
    setItemsPerPage(newItemsPerPage);
    setCurrentPage(1);
  };

  if (!user) {
    return (
      <div className="flex min-h-[50vh] flex-col items-center justify-center gap-4 text-center">
        <h1 className="text-2xl font-semibold text-gray-900">
          Your session has expired
        </h1>
        <p className="text-gray-600">
          Please sign in again to see your personalized dashboard.
        </p>
        <Button href="/auth/login">Sign back in</Button>
      </div>
    );
  }

  const firstName = user.name?.split(" ")?.[0] || "there";

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-gray-900 mb-2">
          Welcome back, {firstName}!
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

          <div
            className="bg-white rounded-lg shadow-sm border border-gray-200 p-6"
            id="now-streaming"
          >
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
            ) : availableForYou.length ? (
              <div>
                <div className="mb-4 flex items-center justify-between text-sm text-gray-600">
                  <div>
                    Showing {startIndex + 1}-
                    {Math.min(endIndex, availableForYou.length)} of{" "}
                    {availableForYou.length} available{" "}
                    {availableForYou.length === 1 ? "title" : "titles"}
                  </div>
                  <div className="flex items-center gap-2">
                    <label
                      htmlFor="items-per-page-dashboard"
                      className="text-sm"
                    >
                      Items per page:
                    </label>
                    <select
                      id="items-per-page-dashboard"
                      value={itemsPerPage}
                      onChange={(e) =>
                        handleItemsPerPageChange(Number(e.target.value))
                      }
                      className="rounded-md border border-gray-300 px-2 py-1 text-sm focus:border-transparent focus:ring-2 focus:ring-blue-500"
                    >
                      <option value={5}>5</option>
                      <option value={10}>10</option>
                      <option value={20}>20</option>
                      <option value={50}>50</option>
                    </select>
                  </div>
                </div>
                <ul className="space-y-4">
                  {paginatedItems.map(({ item, providers }) => (
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
                {totalPages > 1 && (
                  <div className="mt-6 flex items-center justify-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handlePageChange(currentPage - 1)}
                      disabled={currentPage === 1}
                    >
                      Previous
                    </Button>
                    <div className="flex gap-1">
                      {Array.from({ length: totalPages }, (_, i) => i + 1).map(
                        (page) => {
                          // Show first page, last page, current page, and pages around current
                          const showPage =
                            page === 1 ||
                            page === totalPages ||
                            Math.abs(page - currentPage) <= 1;
                          const showEllipsis =
                            (page === 2 && currentPage > 4) ||
                            (page === totalPages - 1 &&
                              currentPage < totalPages - 3);

                          if (!showPage && !showEllipsis) {
                            return null;
                          }

                          if (showEllipsis) {
                            return (
                              <span
                                key={page}
                                className="px-3 py-1 text-sm text-gray-500"
                              >
                                ...
                              </span>
                            );
                          }

                          return (
                            <button
                              key={page}
                              onClick={() => handlePageChange(page)}
                              className={`rounded-md px-3 py-1 text-sm ${
                                page === currentPage
                                  ? "bg-blue-600 text-white"
                                  : "bg-gray-100 text-gray-700 hover:bg-gray-200"
                              }`}
                            >
                              {page}
                            </button>
                          );
                        },
                      )}
                    </div>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handlePageChange(currentPage + 1)}
                      disabled={currentPage === totalPages}
                    >
                      Next
                    </Button>
                  </div>
                )}
              </div>
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
