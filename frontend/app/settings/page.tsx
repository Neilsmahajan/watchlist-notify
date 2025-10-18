"use client";

import { useUser } from "@auth0/nextjs-auth0";
import { LoadingSpinner, Button, ThemedCard } from "@/components/ui";
import { redirect } from "next/navigation";
import Image from "next/image";
import { useCallback, useEffect, useState } from "react";
import {
  formatDisplayDate,
  AVAILABILITY_ACCESS_META,
} from "@/lib/hooks/useWatchlistInsights";

type Service = {
  code: string;
  name: string;
  access?: "subscription" | "free" | "ads";
  active: boolean;
  added_at?: string;
  plan?: string;
};

type ServicesResponse = {
  services?: Service[];
  error?: string;
};

const serviceAssets: Record<string, { logo?: string; fallback?: string }> = {
  netflix: { logo: "/netflix_logo.png" },
  prime_video: { logo: "/prime_video_logo.png" },
  hulu: { logo: "/hulu_logo.png" },
  disney_plus: { logo: "/disney_plus_logo.png" },
  max: { logo: "/hbo_max_logo.png" },
  paramount_plus: { logo: "/paramount_plus_logo.png" },
  peacock: { logo: "/peacock_logo.png" },
  apple_tv_plus: { logo: "/apple_tv_plus_logo.png" },
  pluto_tv: { logo: "/pluto_tv_logo.png" },
  tubi: { logo: "/tubi_logo.png" },
  roku_channel: { logo: "/the_roku_channel_logo.png" },
  plex: { logo: "/plex_logo.png" },
  mgm_plus: { logo: "/mgm_plus_logo.png" },
  crunchyroll: { logo: "/crunchyroll_logo.png" },
  starz: { logo: "/starz_logo.png" },
  amc_plus: { logo: "/amc_plus_logo.png" },
  espn_plus: { logo: "/espn_plus_logo.png" },
  discovery_plus: { logo: "/discovery_plus_logo.png" },
};

export default function Settings() {
  const { user, isLoading } = useUser();
  const [services, setServices] = useState<Service[]>([]);
  const [servicesLoading, setServicesLoading] = useState(true);
  const [servicesError, setServicesError] = useState<string | null>(null);
  const [servicesMessage, setServicesMessage] = useState<string | null>(null);
  const [pendingServices, setPendingServices] = useState<
    Record<string, boolean>
  >({});

  const loadServices = useCallback(
    async (options: { signal?: AbortSignal; silent?: boolean } = {}) => {
      const { signal, silent } = options;
      if (!silent) {
        setServicesLoading(true);
      }
      setServicesError(null);

      try {
        const response = await fetch("/api/me/services", { signal });
        const data = (await response
          .json()
          .catch(() => null)) as ServicesResponse | null;

        if (signal?.aborted) {
          return;
        }

        if (!response.ok) {
          const message = data?.error || "Failed to load services.";
          setServicesError(message);
          setServices([]);
          return;
        }

        const list = Array.isArray(data?.services) ? data.services : [];
        setServices(list);
      } catch (err) {
        if ((err as { name?: string }).name === "AbortError") {
          return;
        }
        console.error("Services fetch error", err);
        setServicesError("Unable to load services. Please try again.");
        setServices([]);
      } finally {
        if (!silent && !signal?.aborted) {
          setServicesLoading(false);
        }
      }
    },
    [],
  );

  useEffect(() => {
    const controller = new AbortController();
    void loadServices({ signal: controller.signal });
    return () => controller.abort();
  }, [loadServices]);

  const handleServicesRefresh = () => {
    setServicesMessage(null);
    void loadServices();
  };

  const handleServiceToggle = async (service: Service) => {
    const code = service.code;
    const desiredState = !service.active;

    setPendingServices((prev) => ({ ...prev, [code]: true }));
    setServicesError(null);
    setServicesMessage(null);

    try {
      const response = await fetch("/api/me/services", {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          toggle: [{ code, active: desiredState }],
        }),
      });

      const data = (await response.json().catch(() => null)) as {
        error?: string;
      } | null;

      if (!response.ok) {
        const message = data?.error || "Failed to update service.";
        setServicesError(message);
        return;
      }

      await loadServices({ silent: true });
      setServicesMessage(
        desiredState
          ? `${service.name} connected successfully.`
          : `${service.name} disconnected successfully.`,
      );
    } catch (err) {
      console.error("Services update error", err);
      setServicesError("Unable to update service. Please retry.");
    } finally {
      setPendingServices((prev) => {
        const next = { ...prev };
        delete next[code];
        return next;
      });
    }
  };

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
    <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-gray-900 mb-2">Settings</h1>
        <p className="text-gray-600">
          Manage your account and notification preferences
        </p>
      </div>

      <div className="space-y-6">
        {/* Profile Section */}
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">
            Profile Information
          </h2>
          <div className="flex items-center space-x-4 mb-6">
            <Image
              src={user.picture || "/default-avatar.svg"}
              alt={user.name || "User"}
              className="w-16 h-16 rounded-full"
              width={64}
              height={64}
            />
            <div>
              <h3 className="text-lg font-medium text-gray-900">{user.name}</h3>
              <p className="text-gray-600">{user.email}</p>
            </div>
          </div>
        </div>

        {/* Streaming Services */}
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">
            Streaming Services
          </h2>
          <p className="text-gray-600 mb-6">
            Connect your streaming services to get personalized availability
            notifications.
          </p>
          <div className="flex items-center justify-between mb-4">
            <div className="space-y-2">
              {servicesMessage && (
                <div className="rounded-md bg-green-50 px-3 py-2 text-sm text-green-700">
                  {servicesMessage}
                </div>
              )}
              {servicesError && (
                <div className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
                  {servicesError}
                </div>
              )}
            </div>
            <Button
              variant="outline"
              size="sm"
              type="button"
              onClick={handleServicesRefresh}
              disabled={servicesLoading}
            >
              Refresh
            </Button>
          </div>

          {servicesLoading ? (
            <div className="flex flex-col items-center gap-3 py-6 text-gray-500">
              <LoadingSpinner size="md" />
              <p>Loading services...</p>
            </div>
          ) : services.length === 0 ? (
            <div className="rounded-lg border border-dashed border-gray-300 p-6 text-center text-gray-600">
              No services available yet.
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
              {services.map((service) => {
                const asset = serviceAssets[service.code];
                const logoSrc = asset?.logo ?? null;
                const fallbackIcon = asset?.fallback ?? "";
                const isPending = Boolean(pendingServices[service.code]);
                const formattedDate = formatDisplayDate(service.added_at);
                const badge = service.access
                  ? AVAILABILITY_ACCESS_META[service.access]
                  : null;

                return (
                  <ThemedCard key={service.code} className="text-center">
                    <div className="mb-2 flex justify-center">
                      {logoSrc ? (
                        <Image
                          src={logoSrc}
                          alt={`${service.name} logo`}
                          width={64}
                          height={64}
                          className="h-16 w-16 object-contain"
                        />
                      ) : (
                        <span
                          className="text-sm font-semibold text-gray-700"
                          aria-hidden="true"
                        >
                          {fallbackIcon || service.name}
                        </span>
                      )}
                    </div>
                    <div className="text-sm font-medium text-gray-900 mb-1">
                      {service.name}
                    </div>
                    {badge ? (
                      <span
                        className={`inline-flex items-center justify-center rounded-full px-2 py-0.5 text-[11px] font-medium ${badge.badgeClass}`}
                      >
                        {badge.label}
                      </span>
                    ) : null}
                    {formattedDate && (
                      <div className="text-xs text-gray-500 mb-2">
                        Added {formattedDate}
                      </div>
                    )}
                    <Button
                      variant={service.active ? "secondary" : "outline"}
                      size="sm"
                      className="w-full"
                      type="button"
                      onClick={() => handleServiceToggle(service)}
                      disabled={isPending}
                      loading={isPending}
                    >
                      {service.active ? "Connected" : "Connect"}
                    </Button>
                  </ThemedCard>
                );
              })}
            </div>
          )}
        </div>

        {/* Notifications */}
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">
            Notifications
          </h2>
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-medium text-gray-900">
                  Email Notifications
                </h3>
                <p className="text-sm text-gray-600">
                  Get notified when content becomes available
                </p>
              </div>
              <button className="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent bg-blue-600 transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-600 focus:ring-offset-2">
                <span className="translate-x-5 pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"></span>
              </button>
            </div>

            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-medium text-gray-900">
                  Weekly Digest
                </h3>
                <p className="text-sm text-gray-600">
                  Get a weekly summary of new content
                </p>
              </div>
              <button className="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent bg-gray-200 transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-600 focus:ring-offset-2">
                <span className="translate-x-0 pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"></span>
              </button>
            </div>

            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-medium text-gray-900">
                  Trending Alerts
                </h3>
                <p className="text-sm text-gray-600">
                  Get notified about trending content in your genres
                </p>
              </div>
              <button className="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent bg-gray-200 transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-600 focus:ring-offset-2">
                <span className="translate-x-0 pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"></span>
              </button>
            </div>
          </div>
        </div>

        {/* Account Actions */}
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">Account</h2>
          <div className="space-y-4">
            <div className="border-t pt-4">
              <Button
                href="/auth/logout"
                variant="secondary"
                className="w-full sm:w-auto"
              >
                Sign Out
              </Button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
