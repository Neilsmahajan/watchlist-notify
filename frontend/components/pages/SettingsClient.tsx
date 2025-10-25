"use client";

import Image from "next/image";
import { useCallback, useEffect, useState } from "react";
import { Button, LoadingSpinner, ThemedCard } from "@/components/ui";
import {
  AVAILABILITY_ACCESS_META,
  formatDisplayDate,
} from "@/lib/hooks/useWatchlistInsights";
import type { SessionUser } from "@/lib/auth/types";
import { redirectToLogin } from "@/lib/auth/client";

export type Service = {
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

type UserPreferences = {
  notify_email?: string;
  use_account_email: boolean;
  marketing_consent: boolean;
  digest_consent: boolean;
  digest: {
    enabled: boolean;
    interval: number;
    interval_unit: "days" | "weeks" | "months";
    last_sent_at?: string;
  };
};

type User = {
  id: string;
  email: string;
  name: string;
  picture: string;
  region: string;
  services?: Service[];
  preferences: UserPreferences;
  created_at: string;
  updated_at: string;
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

interface SettingsClientProps {
  user: SessionUser | null;
}

export default function SettingsClient({ user }: SettingsClientProps) {
  const [services, setServices] = useState<Service[]>([]);
  const [servicesLoading, setServicesLoading] = useState(true);
  const [servicesError, setServicesError] = useState<string | null>(null);
  const [servicesMessage, setServicesMessage] = useState<string | null>(null);
  const [pendingServices, setPendingServices] = useState<
    Record<string, boolean>
  >({});

  // User data state
  const [userData, setUserData] = useState<User | null>(null);
  const [userLoading, setUserLoading] = useState(true);

  // Notification preferences state
  const [notifyEmail, setNotifyEmail] = useState("");
  const [useAccountEmail, setUseAccountEmail] = useState(true);
  const [digestEnabled, setDigestEnabled] = useState(false);
  const [digestInterval, setDigestInterval] = useState(1);
  const [digestIntervalUnit, setDigestIntervalUnit] = useState<
    "days" | "weeks" | "months"
  >("weeks");
  const [digestConsent, setDigestConsent] = useState(false);
  const [marketingConsent, setMarketingConsent] = useState(false);

  // Notification preferences UI state
  const [prefsLoading, setPrefsLoading] = useState(false);
  const [prefsError, setPrefsError] = useState<string | null>(null);
  const [prefsMessage, setPrefsMessage] = useState<string | null>(null);
  const [testEmailLoading, setTestEmailLoading] = useState(false);
  const [testEmailMessage, setTestEmailMessage] = useState<string | null>(null);
  const [testEmailError, setTestEmailError] = useState<string | null>(null);

  const handleUnauthorized = useCallback((response: Response) => {
    if (response.status !== 401) {
      return false;
    }
    redirectToLogin();
    return true;
  }, []);

  const loadUserData = useCallback(
    async (options: { signal?: AbortSignal; silent?: boolean } = {}) => {
      const { signal, silent } = options;
      if (!silent) {
        setUserLoading(true);
      }

      try {
        const response = await fetch("/api/me", { signal });
        if (handleUnauthorized(response)) {
          return;
        }
        const data = (await response.json().catch(() => null)) as User | null;

        if (signal?.aborted) {
          return;
        }

        if (!response.ok || !data) {
          setUserData(null);
          return;
        }

        setUserData(data);
        // Initialize preference form state from loaded user data
        const prefs = data.preferences;
        setNotifyEmail(prefs.notify_email || "");
        setUseAccountEmail(prefs.use_account_email);
        setDigestEnabled(prefs.digest.enabled);
        setDigestInterval(prefs.digest.interval);
        setDigestIntervalUnit(prefs.digest.interval_unit);
        setDigestConsent(prefs.digest_consent);
        setMarketingConsent(prefs.marketing_consent);
      } catch (err) {
        if ((err as { name?: string }).name === "AbortError") {
          return;
        }
        console.error("User data fetch error", err);
        setUserData(null);
      } finally {
        if (!silent && !signal?.aborted) {
          setUserLoading(false);
        }
      }
    },
    [handleUnauthorized],
  );

  const loadServices = useCallback(
    async (options: { signal?: AbortSignal; silent?: boolean } = {}) => {
      const { signal, silent } = options;
      if (!silent) {
        setServicesLoading(true);
      }
      setServicesError(null);

      try {
        const response = await fetch("/api/me/services", { signal });
        if (handleUnauthorized(response)) {
          return;
        }
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
    [handleUnauthorized],
  );

  useEffect(() => {
    if (!user) {
      return;
    }
    const controller = new AbortController();
    void loadUserData({ signal: controller.signal });
    void loadServices({ signal: controller.signal });
    return () => controller.abort();
  }, [loadServices, loadUserData, user]);

  const handleServicesRefresh = () => {
    setServicesMessage(null);
    void loadServices();
  };

  const handlePreferencesSave = async () => {
    setPrefsLoading(true);
    setPrefsError(null);
    setPrefsMessage(null);

    try {
      const body: {
        notify_email?: string;
        use_account_email?: boolean;
        marketing_consent?: boolean;
        digest_consent?: boolean;
        digest_enabled?: boolean;
        digest_interval?: number;
        digest_interval_unit?: "days" | "weeks" | "months";
      } = {};

      // Only send fields that have been modified
      if (userData) {
        if (useAccountEmail !== userData.preferences.use_account_email) {
          body.use_account_email = useAccountEmail;
        }
        if (
          !useAccountEmail &&
          notifyEmail !== userData.preferences.notify_email
        ) {
          body.notify_email = notifyEmail;
        }
        if (digestEnabled !== userData.preferences.digest.enabled) {
          body.digest_enabled = digestEnabled;
        }
        if (digestInterval !== userData.preferences.digest.interval) {
          body.digest_interval = digestInterval;
        }
        if (digestIntervalUnit !== userData.preferences.digest.interval_unit) {
          body.digest_interval_unit = digestIntervalUnit;
        }
        if (digestConsent !== userData.preferences.digest_consent) {
          body.digest_consent = digestConsent;
        }
        if (marketingConsent !== userData.preferences.marketing_consent) {
          body.marketing_consent = marketingConsent;
        }
      } else {
        // If no existing user data, send all values
        body.use_account_email = useAccountEmail;
        if (!useAccountEmail) {
          body.notify_email = notifyEmail;
        }
        body.digest_enabled = digestEnabled;
        body.digest_interval = digestInterval;
        body.digest_interval_unit = digestIntervalUnit;
        body.digest_consent = digestConsent;
        body.marketing_consent = marketingConsent;
      }

      if (Object.keys(body).length === 0) {
        setPrefsMessage("No changes to save.");
        return;
      }

      const response = await fetch("/api/me/preferences", {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify(body),
      });

      if (handleUnauthorized(response)) {
        return;
      }

      const data = (await response.json().catch(() => null)) as
        | { error?: string }
        | User
        | null;

      if (!response.ok) {
        const message =
          (data && "error" in data && data.error) ||
          "Failed to update preferences.";
        setPrefsError(message);
        return;
      }

      // Reload user data to get the latest state
      await loadUserData({ silent: true });
      setPrefsMessage("Preferences updated successfully.");
    } catch (err) {
      console.error("Preferences update error", err);
      setPrefsError("Unable to update preferences. Please retry.");
    } finally {
      setPrefsLoading(false);
    }
  };

  const handleSendTestEmail = async () => {
    setTestEmailLoading(true);
    setTestEmailError(null);
    setTestEmailMessage(null);

    try {
      const response = await fetch("/api/me/notifications/test", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          type: "digest",
        }),
      });

      if (handleUnauthorized(response)) {
        return;
      }

      const data = (await response.json().catch(() => null)) as {
        error?: string;
        message?: string;
        sent_to?: string;
      } | null;

      if (!response.ok) {
        const message = data?.error || "Failed to send test email.";
        setTestEmailError(message);
        return;
      }

      const recipient = data?.sent_to || "your configured email";
      setTestEmailMessage(`Test email sent to ${recipient}!`);
    } catch (err) {
      console.error("Test email error", err);
      setTestEmailError("Unable to send test email. Please retry.");
    } finally {
      setTestEmailLoading(false);
    }
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
      if (handleUnauthorized(response)) {
        return;
      }

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

  if (!user) {
    return (
      <div className="flex min-h-[50vh] flex-col items-center justify-center gap-4 text-center">
        <h1 className="text-2xl font-semibold text-gray-900">
          Sign in to manage settings
        </h1>
        <p className="text-gray-600">
          You need an active session to update notification preferences and
          streaming services.
        </p>
        <Button href="/auth/login">Sign in</Button>
      </div>
    );
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
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">
            Profile Information
          </h2>
          <div className="flex items-center space-x-4 mb-6">
            <Image
              src={
                typeof user.picture === "string" && user.picture.trim().length
                  ? user.picture
                  : "/default-avatar.svg"
              }
              alt={(typeof user.name === "string" && user.name) || "User"}
              className="w-16 h-16 rounded-full"
              width={64}
              height={64}
            />
            <div>
              {typeof user.name === "string" ? (
                <h3 className="text-lg font-medium text-gray-900">
                  {user.name}
                </h3>
              ) : (
                <h3 className="text-lg font-medium text-gray-900">
                  Your Account
                </h3>
              )}
              {typeof user.email === "string" ? (
                <p className="text-gray-600">{user.email}</p>
              ) : null}
            </div>
          </div>
        </div>

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

        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">
            Notification Preferences
          </h2>
          <p className="text-gray-600 mb-6">
            Configure how and when you receive availability notifications.
          </p>

          {userLoading ? (
            <div className="flex flex-col items-center gap-3 py-6 text-gray-500">
              <LoadingSpinner size="md" />
              <p>Loading preferences...</p>
            </div>
          ) : (
            <div className="space-y-6">
              {/* Current Preferences Summary */}
              {userData && (
                <div className="bg-gradient-to-br from-blue-50 to-indigo-50 rounded-lg border border-blue-200 p-5 mb-6">
                  <h3 className="text-sm font-semibold text-gray-900 mb-3 flex items-center gap-2">
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      className="h-5 w-5 text-blue-600"
                      viewBox="0 0 20 20"
                      fill="currentColor"
                    >
                      <path
                        fillRule="evenodd"
                        d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z"
                        clipRule="evenodd"
                      />
                    </svg>
                    Current Notification Settings
                  </h3>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <div className="bg-white rounded-md p-3 border border-blue-100">
                      <dt className="text-xs font-medium text-gray-600 mb-1">
                        Notification Email
                      </dt>
                      <dd className="text-sm font-semibold text-gray-900">
                        {userData.preferences.use_account_email
                          ? userData.email
                          : userData.preferences.notify_email ||
                            "Not configured"}
                      </dd>
                      <dd className="text-xs text-gray-500 mt-0.5">
                        {userData.preferences.use_account_email
                          ? "Using account email"
                          : "Using custom email"}
                      </dd>
                    </div>
                    <div className="bg-white rounded-md p-3 border border-blue-100">
                      <dt className="text-xs font-medium text-gray-600 mb-1">
                        Digest Notifications
                      </dt>
                      <dd className="text-sm font-semibold text-gray-900">
                        {userData.preferences.digest.enabled ? (
                          <span className="text-green-700">Enabled</span>
                        ) : (
                          <span className="text-gray-500">Disabled</span>
                        )}
                      </dd>
                      {userData.preferences.digest.enabled && (
                        <dd className="text-xs text-gray-500 mt-0.5">
                          Every {userData.preferences.digest.interval}{" "}
                          {userData.preferences.digest.interval_unit}
                          {userData.preferences.digest.last_sent_at && (
                            <>
                              {" • Last sent "}
                              {formatDisplayDate(
                                userData.preferences.digest.last_sent_at,
                              )}
                            </>
                          )}
                        </dd>
                      )}
                    </div>
                    <div className="bg-white rounded-md p-3 border border-blue-100">
                      <dt className="text-xs font-medium text-gray-600 mb-1">
                        Digest Consent
                      </dt>
                      <dd className="text-sm font-semibold text-gray-900">
                        {userData.preferences.digest_consent ? (
                          <span className="text-green-700">Granted</span>
                        ) : (
                          <span className="text-gray-500">Not granted</span>
                        )}
                      </dd>
                    </div>
                    <div className="bg-white rounded-md p-3 border border-blue-100">
                      <dt className="text-xs font-medium text-gray-600 mb-1">
                        Marketing Consent
                      </dt>
                      <dd className="text-sm font-semibold text-gray-900">
                        {userData.preferences.marketing_consent ? (
                          <span className="text-green-700">Granted</span>
                        ) : (
                          <span className="text-gray-500">Not granted</span>
                        )}
                      </dd>
                    </div>
                  </div>
                </div>
              )}
              {/* Email Configuration */}
              <div className="border-b pb-6">
                <h3 className="text-sm font-semibold text-gray-900 mb-4">
                  Email Address
                </h3>
                <div className="space-y-3">
                  <label className="flex items-start gap-3">
                    <input
                      type="radio"
                      name="email-choice"
                      checked={useAccountEmail}
                      onChange={() => setUseAccountEmail(true)}
                      className="mt-1 h-4 w-4 text-blue-600 focus:ring-blue-600"
                    />
                    <div className="flex-1">
                      <div className="text-sm font-medium text-gray-900">
                        Use account email
                      </div>
                      <div className="text-sm text-gray-600">
                        {user?.email || "Your Auth0 account email"}
                      </div>
                    </div>
                  </label>
                  <label className="flex items-start gap-3">
                    <input
                      type="radio"
                      name="email-choice"
                      checked={!useAccountEmail}
                      onChange={() => setUseAccountEmail(false)}
                      className="mt-1 h-4 w-4 text-blue-600 focus:ring-blue-600"
                    />
                    <div className="flex-1">
                      <div className="text-sm font-medium text-gray-900 mb-2">
                        Use custom email
                      </div>
                      <input
                        type="email"
                        value={notifyEmail}
                        onChange={(e) => setNotifyEmail(e.target.value)}
                        disabled={useAccountEmail}
                        placeholder="your.email@example.com"
                        className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 disabled:bg-gray-100 disabled:text-gray-500"
                      />
                    </div>
                  </label>
                </div>
              </div>

              {/* Digest Settings */}
              <div className="border-b pb-6">
                <div className="flex items-center justify-between mb-4">
                  <div>
                    <h3 className="text-sm font-semibold text-gray-900">
                      Digest Emails
                    </h3>
                    <p className="text-sm text-gray-600">
                      Receive periodic summaries of available content
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={() => setDigestEnabled(!digestEnabled)}
                    className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-600 focus:ring-offset-2 ${
                      digestEnabled ? "bg-blue-600" : "bg-gray-200"
                    }`}
                  >
                    <span
                      className={`${
                        digestEnabled ? "translate-x-5" : "translate-x-0"
                      } pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out`}
                    ></span>
                  </button>
                </div>

                {digestEnabled && (
                  <div className="space-y-4 pl-4 border-l-2 border-blue-100">
                    <div>
                      <label className="block text-sm font-medium text-gray-900 mb-2">
                        Send digest every
                      </label>
                      <div className="flex gap-3">
                        <input
                          type="number"
                          min="1"
                          max={
                            digestIntervalUnit === "days"
                              ? 31
                              : digestIntervalUnit === "weeks" ||
                                  digestIntervalUnit === "months"
                                ? 12
                                : 31
                          }
                          value={digestInterval}
                          onChange={(e) => {
                            const val = parseInt(e.target.value) || 1;
                            setDigestInterval(Math.max(1, val));
                          }}
                          className="w-20 rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                        />
                        <select
                          value={digestIntervalUnit}
                          onChange={(e) =>
                            setDigestIntervalUnit(
                              e.target.value as "days" | "weeks" | "months",
                            )
                          }
                          className="flex-1 rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                        >
                          <option value="days">Day(s)</option>
                          <option value="weeks">Week(s)</option>
                          <option value="months">Month(s)</option>
                        </select>
                      </div>
                      <p className="mt-1 text-xs text-gray-500">
                        {digestIntervalUnit === "days" && "Maximum: 31 days"}
                        {(digestIntervalUnit === "weeks" ||
                          digestIntervalUnit === "months") &&
                          "Maximum: 12"}
                      </p>
                    </div>

                    <label className="flex items-start gap-3">
                      <input
                        type="checkbox"
                        checked={digestConsent}
                        onChange={(e) => setDigestConsent(e.target.checked)}
                        className="mt-1 h-4 w-4 rounded text-blue-600 focus:ring-blue-600"
                      />
                      <div className="flex-1">
                        <div className="text-sm font-medium text-gray-900">
                          I consent to receive digest emails
                        </div>
                        <div className="text-sm text-gray-600">
                          Required to enable digest notifications
                        </div>
                      </div>
                    </label>
                  </div>
                )}
              </div>

              {/* Marketing Consent */}
              <div className="pb-6">
                <label className="flex items-start gap-3">
                  <input
                    type="checkbox"
                    checked={marketingConsent}
                    onChange={(e) => setMarketingConsent(e.target.checked)}
                    className="mt-1 h-4 w-4 rounded text-blue-600 focus:ring-blue-600"
                  />
                  <div className="flex-1">
                    <div className="text-sm font-medium text-gray-900">
                      Marketing emails
                    </div>
                    <div className="text-sm text-gray-600">
                      Receive updates about new features and improvements
                    </div>
                  </div>
                </label>
              </div>

              {/* Messages */}
              <div className="space-y-2">
                {prefsMessage && (
                  <div className="rounded-md bg-green-50 px-3 py-2 text-sm text-green-700">
                    {prefsMessage}
                  </div>
                )}
                {prefsError && (
                  <div className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
                    {prefsError}
                  </div>
                )}
                {testEmailMessage && (
                  <div className="rounded-md bg-blue-50 px-3 py-2 text-sm text-blue-700">
                    {testEmailMessage}
                  </div>
                )}
                {testEmailError && (
                  <div className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
                    {testEmailError}
                  </div>
                )}
              </div>

              {/* Action Buttons */}
              <div className="flex flex-col sm:flex-row gap-3 pt-4">
                <Button
                  type="button"
                  onClick={handlePreferencesSave}
                  disabled={prefsLoading}
                  loading={prefsLoading}
                  className="flex-1 sm:flex-initial"
                >
                  Save Preferences
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  onClick={handleSendTestEmail}
                  disabled={testEmailLoading || prefsLoading}
                  loading={testEmailLoading}
                  className="flex-1 sm:flex-initial"
                >
                  Send Test Email
                </Button>
              </div>
            </div>
          )}
        </div>

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
