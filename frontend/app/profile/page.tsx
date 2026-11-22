import type { Metadata } from "next";
import Link from "next/link";
import ServiceLogo from "@/components/ServiceLogo";
import { Button, Notice, PageShell } from "@/components/ui";
import { getMe, getServices, getWatchlist } from "@/lib/api/backend";
import { requireSession } from "@/lib/auth/session";
import { formatDate } from "@/lib/format";
import { serviceName } from "@/lib/services";
import { totalOf } from "@/lib/watchlist/types";

export const metadata: Metadata = {
  title: "Profile",
  robots: { index: false },
};

// The account at a glance. Counts come from one request for a single row:
// the API reports totals alongside it, so the list is not downloaded.
export default async function ProfilePage() {
  const { user, token } = await requireSession("/profile");
  const [me, counts, services] = await Promise.all([
    getMe(token),
    getWatchlist(token, "limit=1"),
    getServices(token),
  ]);

  const account = me.ok ? me.data : null;
  const name = account?.name || user.name || "Your account";
  const email = account?.email || user.email || "";
  const picture = account?.picture || user.picture || "";
  const since = formatDate(account?.created_at);
  const digest = account?.preferences?.digest;

  const facets = counts.ok ? (counts.data.meta.facets ?? {}) : {};
  const total = counts.ok ? totalOf(counts.data.meta) : 0;
  const mine = services.ok ? (services.data.services ?? []).filter((s) => s.active) : [];

  const stats = [
    { label: "Titles", value: total, href: "/watchlist" },
    { label: "On your services", value: facets.available ?? 0, href: "/watchlist?available=true" },
    { label: "Watching", value: facets.status?.watching ?? 0, href: "/watchlist?status=watching" },
    { label: "Finished", value: facets.status?.finished ?? 0, href: "/watchlist?status=finished" },
  ];

  return (
    <PageShell width="medium">
      <header className="flex flex-col gap-6 sm:flex-row sm:items-center">
        <span className="flex size-24 shrink-0 items-center justify-center overflow-hidden rounded-full bg-accent font-display text-4xl text-accent-ink ring-4 ring-surface">
          {picture ? (
            <img
              src={picture}
              alt=""
              referrerPolicy="no-referrer"
              className="size-full object-cover"
            />
          ) : (
            (name.trim()[0] ?? "W").toUpperCase()
          )}
        </span>
        <div className="min-w-0 flex-1">
          <p className="eyebrow">Profile</p>
          <h1 className="display mt-3 truncate text-4xl sm:text-5xl">{name}</h1>
          <p className="mt-2 truncate text-muted">{email}</p>
          <p className="slate mt-3 text-faint">
            {[since && `Member since ${since}`, account?.region && `Region ${account.region}`]
              .filter(Boolean)
              .join(" · ")}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button href="/settings" variant="secondary">
            Settings
          </Button>
          <Button href="/auth/logout" variant="ghost">
            Sign out
          </Button>
        </div>
      </header>

      {!me.ok && (
        <Notice tone="error" className="mt-8">
          {me.error}
        </Notice>
      )}
      {!counts.ok && (
        <Notice tone="error" className="mt-8">
          {counts.error}
        </Notice>
      )}

      <section aria-labelledby="numbers" className="mt-12">
        <h2 id="numbers" className="display text-2xl sm:text-3xl">
          Your list in numbers
        </h2>
        <ul className="mt-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
          {stats.map((stat) => (
            <li key={stat.label}>
              <Link
                href={stat.href}
                className="block rounded-2xl border border-line bg-surface p-4 transition-colors hover:border-line-strong sm:p-5"
              >
                <span className="slate block text-muted">{stat.label}</span>
                <span className="display mt-2 block text-4xl sm:text-5xl">
                  {stat.value.toLocaleString("en-US")}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </section>

      <div className="mt-12 grid gap-x-16 gap-y-12 lg:grid-cols-2">
        <section aria-labelledby="services">
          <div className="flex items-baseline justify-between gap-4">
            <h2 id="services" className="display text-2xl sm:text-3xl">
              Your services
            </h2>
            <Link
              href="/settings#services"
              className="shrink-0 text-sm text-accent-text underline-offset-4 hover:underline"
            >
              Edit
            </Link>
          </div>
          {!services.ok ? (
            <Notice tone="error" className="mt-5">
              {services.error}
            </Notice>
          ) : mine.length === 0 ? (
            <p className="mt-4 text-sm leading-relaxed text-muted">
              None chosen yet. Pick the services you have and your watchlist will show where
              each title is streaming.
            </p>
          ) : (
            <ul className="mt-5 flex flex-wrap gap-2">
              {mine.map((service) => {
                const display = serviceName(service.code, undefined, service.name);
                return (
                  <li
                    key={service.code}
                    className="flex items-center gap-2.5 rounded-full border border-line bg-surface py-1.5 pl-1.5 pr-4 text-sm"
                  >
                    <ServiceLogo code={service.code} name={display} labelled className="size-7 rounded-full" />
                    {display}
                  </li>
                );
              })}
            </ul>
          )}
        </section>

        <section aria-labelledby="digest">
          <div className="flex items-baseline justify-between gap-4">
            <h2 id="digest" className="display text-2xl sm:text-3xl">
              Email digest
            </h2>
            <Link
              href="/settings#notifications"
              className="shrink-0 text-sm text-accent-text underline-offset-4 hover:underline"
            >
              Edit
            </Link>
          </div>
          <p className="mt-4 text-sm leading-relaxed text-muted">
            {digest?.enabled ? (
              <>
                On, every{" "}
                {digest.interval === 1
                  ? (digest.interval_unit ?? "weeks").replace(/s$/, "")
                  : `${digest.interval} ${digest.interval_unit}`}
                .
                {formatDate(digest.last_sent_at) && ` Last sent ${formatDate(digest.last_sent_at)}.`}
                {formatDate(digest.next_scheduled_at) &&
                  ` Next due ${formatDate(digest.next_scheduled_at)}.`}
              </>
            ) : (
              "Off. Turn it on to get an email when titles on your list are streaming on your services."
            )}
          </p>
        </section>
      </div>
    </PageShell>
  );
}
