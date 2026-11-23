import type { Metadata } from "next";
import WatchlistBrowser from "@/components/watchlist/WatchlistBrowser";
import { Button, EmptyState, Notice, PageHeading, PageShell } from "@/components/ui";
import { IconPlus } from "@/components/icons";
import { getServices, getWatchlist } from "@/lib/api/backend";
import { requireSession } from "@/lib/auth/session";
import { filtersToApiQuery, filtersToSearch, parseFilters } from "@/lib/watchlist/query";

export const metadata: Metadata = {
  title: "Watchlist",
  robots: { index: false },
};

type PageProps = {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

export default async function WatchlistPage({ searchParams }: PageProps) {
  const filters = parseFilters(await searchParams);
  const query = filtersToSearch(filters);
  const { token } = await requireSession(query ? `/watchlist?${query}` : "/watchlist");

  // One page of the list, for the filters in the address, plus the service
  // names. The browser asks for more pages as they are scrolled to.
  const [list, services] = await Promise.all([
    getWatchlist(token, filtersToApiQuery(filters)),
    getServices(token),
  ]);

  const catalogue = services.ok ? (services.data.services ?? []) : [];
  const hasServices = catalogue.some((service) => service.active);

  return (
    <PageShell>
      <PageHeading
        title="Your watchlist"
        action={
          <>
            <Button href="/settings#import" variant="secondary">
              Import
            </Button>
            <Button href="/search">
              <IconPlus className="size-4" />
              Add titles
            </Button>
          </>
        }
      />

      {services.ok && !hasServices && (
        <Notice
          tone="accent"
          className="mb-6"
          action={
            <Button href="/settings#services" size="sm" variant="ink">
              Choose services
            </Button>
          }
        >
          Tell us which streaming services you have, and each title will show
          where you can watch it.
        </Notice>
      )}

      <WatchlistBrowser
        endpoint="/api/watchlist"
        basePath="/watchlist"
        initialPage={list.ok ? list.data : null}
        initialError={list.ok ? null : list.error}
        services={catalogue}
        emptyState={
          <EmptyState
            title="Nothing on your list yet"
            action={
              <>
                <Button href="/search">Find something to watch</Button>
                <Button href="/settings#import" variant="secondary">
                  Import from IMDb
                </Button>
              </>
            }
          >
            Search for a film or series and add it, or bring a list over from
            IMDb. Each title then shows which of your services has it.
          </EmptyState>
        }
      />
    </PageShell>
  );
}
