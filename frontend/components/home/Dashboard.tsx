import Link from "next/link";
import type { ReactNode } from "react";
import TitleShelf from "@/components/home/TitleShelf";
import TonightPanel from "@/components/home/TonightPanel";
import ServiceLogo from "@/components/ServiceLogo";
import { Button, EmptyState, Notice, PageShell } from "@/components/ui";
import { IconArrowRight, IconPlus } from "@/components/icons";
import { getMe, getServices, getWatchlist } from "@/lib/api/backend";
import type { SessionUser } from "@/lib/auth/types";
import { serviceName, type ServiceNames } from "@/lib/services";
import { STATUS_LABELS, totalOf, type WatchlistStatus } from "@/lib/watchlist/types";

const SHELF = 14;
const STATUSES: WatchlistStatus[] = ["planned", "watching", "finished"];

function firstName(user: SessionUser): string | null {
  const name = (user.name ?? "").trim();
  if (!name || name.includes("@")) return null;
  return name.split(/\s+/)[0];
}

function Section({
  title,
  href,
  linkLabel,
  children,
}: {
  title: string;
  href?: string;
  linkLabel?: string;
  children: ReactNode;
}) {
  return (
    <section className="mt-12 sm:mt-16">
      <div className="mb-5 flex items-baseline justify-between gap-4">
        <h2 className="display text-2xl sm:text-3xl">{title}</h2>
        {href && (
          <Link
            href={href}
            className="inline-flex shrink-0 items-center gap-1 text-sm text-accent-text underline-offset-4 hover:underline"
          >
            {linkLabel}
            <IconArrowRight className="size-3.5" />
          </Link>
        )}
      </div>
      {children}
    </section>
  );
}

// The signed-in home page. It is rendered on the server from a few small
// requests made together: counts for the whole list, a dozen titles for each
// row, the services, and the profile, which says whether this API offers
// tonight's picks. The list itself is never downloaded here.
export default async function Dashboard({ user, token }: { user: SessionUser; token: string }) {
  const [overall, ready, watching, services, me] = await Promise.all([
    getWatchlist(token, "limit=1"),
    getWatchlist(token, `available=true&status=planned&sort=-added_at&limit=${SHELF}`),
    getWatchlist(token, `status=watching&sort=-added_at&limit=${SHELF}`),
    getServices(token),
    getMe(token),
  ]);

  const name = firstName(user);
  const greeting = name ? `Welcome back, ${name}` : "Welcome back";

  if (!overall.ok) {
    return (
      <PageShell>
        <p className="eyebrow">{greeting}</p>
        <h1 className="display mt-4 text-4xl sm:text-6xl">Your list is out of reach right now.</h1>
        <Notice
          tone="error"
          className="mt-8"
          action={
            <Button href="/" size="sm" variant="secondary">
              Try again
            </Button>
          }
        >
          {overall.error}
        </Notice>
      </PageShell>
    );
  }

  const facets = overall.data.meta.facets ?? {};
  const total = totalOf(overall.data.meta);
  const catalogue = services.ok ? (services.data.services ?? []) : [];
  const mine = catalogue.filter((service) => service.active);
  const names: ServiceNames = Object.fromEntries(catalogue.map((s) => [s.code, s.name]));

  const readyItems = ready.ok ? (ready.data.items ?? []) : [];
  const readyCount = ready.ok ? ready.data.meta.total : 0;
  const watchingItems = watching.ok ? (watching.data.items ?? []) : [];
  const watchingCount = watching.ok ? watching.data.meta.total : 0;
  const onServices = facets.available ?? 0;

  // "What should I watch tonight?" is drawn only when the API says it has the
  // feature: an API without an Anthropic key, an older API and a profile that
  // could not be read all leave it out. With no services chosen the notice
  // below already says what to do, so the panel would only repeat it.
  const noServices = services.ok && mine.length === 0;
  const showTonight = me.ok && me.data.features?.recommendations === true && !noServices;

  const perService = mine
    .map((service) => ({ ...service, count: facets.services?.[service.code] ?? 0 }))
    .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name, "en"));
  const most = Math.max(1, ...perService.map((service) => service.count));

  if (total === 0) {
    return (
      <PageShell>
        <p className="eyebrow">{greeting}</p>
        <h1 className="display mt-4 max-w-3xl text-4xl sm:text-6xl">
          Start with one title you keep meaning to watch.
        </h1>
        <EmptyState
          className="mt-10"
          title="Your watchlist is empty"
          action={
            <>
              <Button href="/search">
                <IconPlus className="size-4" />
                Find a title
              </Button>
              <Button href="/settings#import" variant="secondary">
                Import from IMDb
              </Button>
              {mine.length === 0 && (
                <Button href="/settings#services" variant="secondary">
                  Choose your services
                </Button>
              )}
            </>
          }
        >
          Add films and series, tell us which streaming services you have, and
          this page becomes the answer to “what can I watch tonight?”
        </EmptyState>
      </PageShell>
    );
  }

  let headline: ReactNode;
  if (noServices) {
    headline = <>Tell us what you subscribe to, and we will tell you what is on.</>;
  } else if (readyCount > 0) {
    headline = (
      <>
        <em className="text-accent-text">{readyCount.toLocaleString("en-US")}</em>{" "}
        {readyCount === 1 ? "title" : "titles"} on your list{" "}
        {readyCount === 1 ? "is" : "are"} ready to watch tonight.
      </>
    );
  } else if (onServices > 0) {
    headline = <>You have watched everything your services have. Time to add more.</>;
  } else {
    headline = <>Nothing on your list is on your services yet. We are watching for it.</>;
  }

  return (
    <PageShell>
      <header className="flex flex-wrap items-end justify-between gap-x-10 gap-y-6">
        <div className="min-w-0 max-w-4xl">
          <p className="eyebrow">{greeting}</p>
          <h1 className="display mt-4 text-[2.5rem] sm:text-6xl lg:text-7xl">{headline}</h1>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button href="/watchlist" variant="secondary">
            Open watchlist
          </Button>
          <Button href="/search">
            <IconPlus className="size-4" />
            Add titles
          </Button>
        </div>
      </header>

      {noServices && (
        <Notice
          tone="accent"
          className="mt-8"
          action={
            <Button href="/settings#services" size="sm" variant="ink">
              Choose services
            </Button>
          }
        >
          You have {total.toLocaleString("en-US")} {total === 1 ? "title" : "titles"} saved and
          no streaming services chosen, so nothing can be matched yet.
        </Notice>
      )}

      {!ready.ok && (
        <Notice tone="error" className="mt-8">
          {ready.error}
        </Notice>
      )}

      {showTonight && <TonightPanel names={names} />}

      {readyItems.length > 0 && (
        <Section
          title="Ready to watch"
          href="/watchlist?status=planned&available=true"
          linkLabel={`All ${readyCount.toLocaleString("en-US")}`}
        >
          <TitleShelf items={readyItems} names={names} label="Ready to watch" />
        </Section>
      )}

      {watchingItems.length > 0 && (
        <Section
          title="Keep watching"
          href="/watchlist?status=watching"
          linkLabel={`All ${watchingCount.toLocaleString("en-US")}`}
        >
          <TitleShelf items={watchingItems} names={names} label="Keep watching" />
        </Section>
      )}

      <div className="mt-12 grid gap-x-16 gap-y-12 sm:mt-16 lg:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]">
        <section aria-labelledby="by-service">
          <div className="mb-5 flex items-baseline justify-between gap-4">
            <h2 id="by-service" className="display text-2xl sm:text-3xl">
              On your services
            </h2>
            <Link
              href="/settings#services"
              className="shrink-0 text-sm text-accent-text underline-offset-4 hover:underline"
            >
              Edit services
            </Link>
          </div>
          {perService.length === 0 ? (
            <p className="text-sm leading-relaxed text-muted">
              No services chosen yet. Pick the ones you pay for and each gets a
              row here with the titles it has.
            </p>
          ) : (
            <ul className="divide-y divide-line border-y border-line">
              {perService.map((service) => {
                const display = serviceName(service.code, names, service.name);
                return (
                  <li key={service.code}>
                    <Link
                      href={`/watchlist?service=${service.code}`}
                      className="group flex items-center gap-4 py-3 transition-colors hover:bg-surface-2/60"
                    >
                      <ServiceLogo code={service.code} name={display} labelled className="size-9" />
                      <span className="w-28 shrink-0 truncate text-sm font-medium sm:w-40">
                        {display}
                      </span>
                      <span className="relative hidden h-1.5 flex-1 overflow-hidden rounded-full bg-surface-2 min-[420px]:block">
                        <span
                          className="absolute inset-y-0 left-0 rounded-full bg-accent"
                          style={{ width: `${(service.count / most) * 100}%` }}
                        />
                      </span>
                      <span className="ml-auto w-10 text-right font-mono text-sm tabular-nums">
                        {service.count.toLocaleString("en-US")}
                      </span>
                      <IconArrowRight className="size-4 shrink-0 text-faint transition-transform group-hover:translate-x-0.5 group-hover:text-fg" />
                    </Link>
                  </li>
                );
              })}
            </ul>
          )}
        </section>

        <section aria-labelledby="the-list">
          <h2 id="the-list" className="display mb-5 text-2xl sm:text-3xl">
            The whole list
          </h2>
          <ul className="grid grid-cols-3 gap-3">
            {STATUSES.map((status) => (
              <li key={status}>
                <Link
                  href={`/watchlist?status=${status}`}
                  className="block rounded-2xl border border-line bg-surface p-4 transition-colors hover:border-line-strong"
                >
                  <span className="slate block text-muted">{STATUS_LABELS[status]}</span>
                  <span className="display mt-2 block text-4xl sm:text-5xl">
                    {(facets.status?.[status] ?? 0).toLocaleString("en-US")}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
          <p className="mt-5 text-sm leading-relaxed text-muted">
            {total.toLocaleString("en-US")} {total === 1 ? "title" : "titles"} in all.{" "}
            {onServices.toLocaleString("en-US")} on your services,{" "}
            {(facets.unavailable ?? 0).toLocaleString("en-US")} not yet. Availability is checked
            every day, and new arrivals go into your{" "}
            <Link href="/settings#notifications" className="text-accent-text underline-offset-4 hover:underline">
              email digest
            </Link>
            .
          </p>
        </section>
      </div>
    </PageShell>
  );
}
