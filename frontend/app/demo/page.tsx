import type { Metadata } from "next";
import DemoNote from "@/components/watchlist/DemoNote";
import WatchlistBrowser from "@/components/watchlist/WatchlistBrowser";
import ServiceLogo from "@/components/ServiceLogo";
import { Button, EmptyState, PageShell } from "@/components/ui";
import { IconArrowRight } from "@/components/icons";
import { getDemoServices, getDemoWatchlist } from "@/lib/api/backend";
import { getSessionUser } from "@/lib/auth/session";
import { filtersToApiQuery, parseFilters } from "@/lib/watchlist/query";
import { serviceName } from "@/lib/services";
import { totalOf } from "@/lib/watchlist/types";

const WORDS = ["no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"];
const countWord = (n: number) => (n > 0 && n < WORDS.length ? WORDS[n] : n > 0 ? String(n) : "a viewer's");

export const metadata: Metadata = {
  title: "Live demo",
  description:
    "A sample watchlist of films and series, checked against a viewer's streaming services. Filter it, open a title, see where it streams. No account needed.",
  alternates: { canonical: "/demo" },
};

type PageProps = {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

// The public demo: the same grid, filters and detail view as a real
// watchlist, read-only, over one seeded account. It needs no session and
// cannot reach anyone's data: it only ever calls the API's /api/demo routes.
export default async function DemoPage({ searchParams }: PageProps) {
  const filters = parseFilters(await searchParams);
  const [list, services, user] = await Promise.all([
    getDemoWatchlist(filtersToApiQuery(filters)),
    getDemoServices(),
    getSessionUser(),
  ]);

  const cta = user
    ? { href: "/watchlist", label: "Open your watchlist" }
    : { href: "/auth/login", label: "Make your own list" };

  if (!list.ok) {
    return (
      <PageShell width="narrow" className="py-20">
        <EmptyState
          title="The demo is taking a break"
          action={
            <>
              <Button href={cta.href}>{cta.label}</Button>
              <Button href="/" variant="secondary">
                Back to the start
              </Button>
            </>
          }
        >
          {list.status === 404
            ? "The sample watchlist has not been set up on this server yet. The real thing works: an account is free and takes a minute."
            : "The sample watchlist could not be loaded just now. Try again in a minute, or skip straight to the real thing."}
        </EmptyState>
      </PageShell>
    );
  }

  const catalogue = services.ok ? (services.data.services ?? []) : [];
  const theirs = catalogue.filter((service) => service.active);
  const total = totalOf(list.data.meta);

  return (
    <PageShell>
      <section
        aria-label="About this demo"
        className="mb-8 overflow-hidden rounded-2xl border border-accent/40 bg-accent-soft sm:mb-10"
      >
        <div className="flex flex-col gap-4 p-4 sm:gap-5 sm:p-7 lg:flex-row lg:items-end lg:justify-between">
          <div className="min-w-0 max-w-2xl">
            <p className="eyebrow">Live demo · read-only</p>
            <h1 className="display mt-2.5 text-[1.7rem] sm:text-[2.75rem]">
              A sample watchlist, checked against {countWord(theirs.length)} services.
            </h1>
            {/* A phone gets the short version, so the posters are on the
                first screen. */}
            <p className="mt-2.5 text-sm leading-relaxed text-muted sm:hidden">
              Each poster shows where it streams for this viewer. Tap one, or try a filter.
            </p>
            <p className="mt-3 hidden text-[15px] leading-relaxed text-muted sm:block">
              This is what a list looks like once it is yours: every title
              shows where it is streaming on the services this viewer pays for.
              Filter it, open a title, share the address. Nothing here can be
              changed.
            </p>
          </div>
          <div className="flex shrink-0 flex-wrap items-center justify-between gap-x-4 gap-y-3 lg:flex-col lg:items-end">
            {theirs.length > 0 && (
              <ul className="flex flex-wrap items-center gap-1.5 sm:gap-2" aria-label="This viewer's services">
                {theirs.map((service) => (
                  <li key={service.code}>
                    <ServiceLogo
                      code={service.code}
                      name={serviceName(service.code, undefined, service.name)}
                      className="size-7 sm:size-8"
                    />
                  </li>
                ))}
              </ul>
            )}
            <Button href={cta.href} variant="ink">
              {cta.label}
              <IconArrowRight className="size-4" />
            </Button>
          </div>
        </div>
      </section>

      <WatchlistBrowser
        endpoint="/api/demo/watchlist"
        basePath="/demo"
        initialPage={list.data}
        services={catalogue}
        readOnly
        readOnlyNote={<DemoNote href={cta.href} label={cta.label} />}
        emptyState={
          <EmptyState title="The demo list is empty" action={<Button href={cta.href}>{cta.label}</Button>}>
            The sample account has no titles on this server yet.
          </EmptyState>
        }
      />

      {total > 0 && (
        <p className="mt-12 text-center text-sm text-muted">
          {total.toLocaleString("en-US")} titles, checked daily.{" "}
          <a href={cta.href} className="font-medium text-accent-text underline-offset-4 hover:underline">
            {cta.label}
          </a>
        </p>
      )}
    </PageShell>
  );
}
