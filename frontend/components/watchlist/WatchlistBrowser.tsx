"use client";

import { useSearchParams } from "next/navigation";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import FilterBar from "@/components/watchlist/FilterBar";
import TitleDialog, { type DialogActions } from "@/components/watchlist/TitleDialog";
import TitleTile from "@/components/watchlist/TitleTile";
import { Button, EmptyState, Notice, Spinner, cx } from "@/components/ui";
import { redirectToLogin } from "@/lib/auth/client";
import { readError, recheckItem, removeItem, setItemStatus } from "@/lib/watchlist/client";
import type { ServiceNames } from "@/lib/services";
import {
  NO_FILTERS,
  PAGE_SIZE,
  filtersToApiQuery,
  filtersToSearch,
  hasActiveFilters,
  parseFilters,
  type Filters,
} from "@/lib/watchlist/query";
import type {
  Service,
  WatchlistItem,
  WatchlistMeta,
  WatchlistPage,
  WatchlistStatus,
} from "@/lib/watchlist/types";

type WatchlistBrowserProps = {
  // Where pages of the list come from: the user's own list or the demo's.
  endpoint: "/api/watchlist" | "/api/demo/watchlist";
  // The page this sits on, for writing filters to the address bar.
  basePath: string;
  // The first page, rendered on the server for the filters in the address.
  initialPage: WatchlistPage | null;
  initialError?: string | null;
  services: Service[];
  // The demo: no status changes, no removing.
  readOnly?: boolean;
  readOnlyNote?: ReactNode;
  // What to show when the list has nothing in it at all.
  emptyState: ReactNode;
};

// What is on screen: the items loaded so far for one set of filters.
type Loaded = {
  key: string; // the filters these items belong to, as a query string
  items: WatchlistItem[];
  meta: WatchlistMeta;
  // How many of the loaded items the server would still return for these
  // filters. It is where the next page starts; it drops below items.length
  // when an item is removed or stops matching.
  offset: number;
  // Set when a page came back empty, so "load more" cannot loop.
  exhausted: boolean;
};

class HttpError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}

function mergeItems(current: WatchlistItem[], more: WatchlistItem[]): WatchlistItem[] {
  const seen = new Set(current.map((item) => item.id));
  return [...current, ...more.filter((item) => !seen.has(item.id))];
}

// The watchlist as a poster grid with filters, used by the user's own list
// and by the public demo. The filters are the address bar's query string:
// changing one pushes a history entry, so views can be shared and the back
// button undoes a filter. The server renders the first page; this component
// fetches the rest, 48 at a time.
export default function WatchlistBrowser({
  endpoint,
  basePath,
  initialPage,
  initialError = null,
  services,
  readOnly = false,
  readOnlyNote,
  emptyState,
}: WatchlistBrowserProps) {
  const searchParams = useSearchParams();
  const filters = useMemo(() => parseFilters(searchParams), [searchParams]);
  const key = filtersToSearch(filters);

  const [data, setData] = useState<Loaded | null>(() =>
    initialPage
      ? {
          key,
          items: initialPage.items ?? [],
          meta: initialPage.meta,
          offset: initialPage.items?.length ?? 0,
          exhausted: false,
        }
      : null,
  );
  const [error, setError] = useState<string | null>(initialError);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);

  const names = useMemo<ServiceNames>(
    () => Object.fromEntries(services.map((s) => [s.code, s.name])),
    [services],
  );
  const mine = useMemo(
    () => new Set(services.filter((s) => s.active).map((s) => s.code)),
    [services],
  );

  const fetchPage = useCallback(
    async (
      forFilters: Filters,
      offset: number,
      limit: number = PAGE_SIZE,
      signal?: AbortSignal,
    ): Promise<WatchlistPage> => {
      const response = await fetch(
        `${endpoint}?${filtersToApiQuery(forFilters, offset, limit)}`,
        { headers: { Accept: "application/json" }, signal },
      );
      if (response.status === 401 && !readOnly) {
        redirectToLogin();
        throw new HttpError("Your session has expired. Signing you in again.", 401);
      }
      if (!response.ok) {
        throw new HttpError(
          await readError(response, "The list could not be loaded."),
          response.status,
        );
      }
      const page = (await response.json()) as WatchlistPage;
      return { items: page.items ?? [], meta: page.meta };
    },
    [endpoint, readOnly],
  );

  // Load the first page whenever the filters are not the ones on screen.
  const shownKey = data?.key;
  useEffect(() => {
    if (shownKey === key && retry === 0) return;
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    setMoreError(null);
    fetchPage(filters, 0, PAGE_SIZE, controller.signal)
      .then((page) => {
        setData({
          key,
          items: page.items,
          meta: page.meta,
          offset: page.items.length,
          exhausted: false,
        });
        setLoading(false);
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setError(cause instanceof Error ? cause.message : "The list could not be loaded.");
        setLoading(false);
      });
    return () => controller.abort();
    // `filters` is derived from `key`; `shownKey` only gates the first run.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, retry, fetchPage]);

  const setFilters = useCallback(
    (next: Filters, options?: { replace?: boolean }) => {
      const query = filtersToSearch(next);
      const url = query ? `${basePath}?${query}` : basePath;
      // The Next router follows native history calls: useSearchParams above
      // updates, without a round trip to the server for the page itself.
      if (options?.replace) {
        window.history.replaceState(null, "", url);
      } else {
        window.history.pushState(null, "", url);
      }
    },
    [basePath],
  );

  const current = data && data.key === key ? data : null;
  const canLoadMore = Boolean(
    current && !current.exhausted && current.offset < current.meta.total,
  );

  const loadMore = useCallback(async () => {
    if (!current || loading || loadingMore || !canLoadMore) return;
    setLoadingMore(true);
    setMoreError(null);
    try {
      const page = await fetchPage(filters, current.offset);
      setData((latest) =>
        latest && latest.key === current.key
          ? {
              ...latest,
              items: mergeItems(latest.items, page.items),
              meta: page.meta,
              offset: latest.offset + page.items.length,
              exhausted: page.items.length === 0,
            }
          : latest,
      );
    } catch (cause) {
      setMoreError(cause instanceof Error ? cause.message : "More titles could not be loaded.");
    } finally {
      setLoadingMore(false);
    }
  }, [current, loading, loadingMore, canLoadMore, fetchPage, filters]);

  // Load the next page as the end of the grid comes into view. The button
  // stays for keyboards, and for when a load fails.
  const sentinel = useRef<HTMLDivElement>(null);
  const loadMoreRef = useRef(loadMore);
  loadMoreRef.current = loadMore;
  useEffect(() => {
    const node = sentinel.current;
    if (!node || !canLoadMore || moreError) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) void loadMoreRef.current();
      },
      { rootMargin: "800px 0px" },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [canLoadMore, moreError, current?.items.length]);

  // Counts and totals after a change, without reloading the items.
  const refreshMeta = useCallback(async () => {
    try {
      const page = await fetchPage(filters, 0, 1);
      setData((latest) => (latest && latest.key === key ? { ...latest, meta: page.meta } : latest));
    } catch {
      // The counts stay as they were until the next load.
    }
  }, [fetchPage, filters, key]);

  const actions = useMemo<DialogActions | undefined>(() => {
    if (readOnly) return undefined;

    return {
      async onStatus(item: WatchlistItem, status: WatchlistStatus) {
        await setItemStatus(item, status);
        // The item stays on screen even if it no longer matches the status
        // filter, so the change is visible; it only stops counting towards
        // where the next page starts.
        const leaves = Boolean(filters.status) && filters.status !== status;
        setData((latest) =>
          latest
            ? {
                ...latest,
                items: latest.items.map((i) => (i.id === item.id ? { ...i, status } : i)),
                offset: leaves ? Math.max(0, latest.offset - 1) : latest.offset,
              }
            : latest,
        );
        void refreshMeta();
      },

      async onRemove(item: WatchlistItem) {
        await removeItem(item);
        setSelectedId(null);
        setData((latest) =>
          latest
            ? {
                ...latest,
                items: latest.items.filter((i) => i.id !== item.id),
                offset: Math.max(0, latest.offset - 1),
              }
            : latest,
        );
        void refreshMeta();
      },

      async onRecheck(item: WatchlistItem) {
        const fresh = await recheckItem(item);
        if (fresh) {
          setData((latest) =>
            latest
              ? { ...latest, items: latest.items.map((i) => (i.id === fresh.id ? fresh : i)) }
              : latest,
          );
        }
        void refreshMeta();
      },
    };
  }, [readOnly, filters.status, refreshMeta]);

  const items = data?.items ?? [];
  const selected = selectedId ? (items.find((item) => item.id === selectedId) ?? null) : null;
  const filtered = hasActiveFilters(filters);
  const total = current?.meta.total ?? data?.meta.total ?? 0;
  const facets = (current ?? data)?.meta.facets;

  // An empty list with no filters is a new account (or an unseeded demo).
  const nothingAtAll = Boolean(current) && !filtered && total === 0 && items.length === 0;

  if (!data && error) {
    return (
      <Notice
        tone="error"
        action={
          <Button size="sm" variant="secondary" onClick={() => setRetry((n) => n + 1)} loading={loading}>
            Try again
          </Button>
        }
      >
        {error}
      </Notice>
    );
  }

  if (nothingAtAll) {
    return <>{emptyState}</>;
  }

  return (
    <div>
      <FilterBar filters={filters} facets={facets} names={names} mine={mine} onChange={setFilters} />

      <div className="mb-4 mt-6 flex min-h-6 items-center justify-between gap-4">
        <p className="slate text-muted" aria-live="polite">
          {loading ? (
            <span className="inline-flex items-center gap-2">
              <Spinner className="size-3.5" /> Loading
            </span>
          ) : (
            <>
              {total.toLocaleString("en-US")} {total === 1 ? "title" : "titles"}
              {filtered ? " match" : ""}
            </>
          )}
        </p>
        {filtered && (
          <button
            type="button"
            onClick={() => setFilters({ ...NO_FILTERS, sort: filters.sort })}
            className="text-[13px] text-accent-text underline-offset-4 hover:underline"
          >
            Clear filters
          </button>
        )}
      </div>

      {error && (
        <Notice
          tone="error"
          className="mb-6"
          action={
            <Button size="sm" variant="secondary" onClick={() => setRetry((n) => n + 1)}>
              Try again
            </Button>
          }
        >
          {error}
        </Notice>
      )}

      {items.length === 0 && !loading && !error ? (
        <EmptyState
          title="Nothing matches"
          action={
            <Button variant="secondary" onClick={() => setFilters({ ...NO_FILTERS, sort: filters.sort })}>
              Clear filters
            </Button>
          }
        >
          No title on this list fits those filters. Loosen one, or clear them
          all.
        </EmptyState>
      ) : (
        <ul
          className={cx("poster-grid transition-opacity duration-200", loading && "opacity-45")}
          aria-busy={loading}
        >
          {items.map((item, index) => (
            <TitleTile
              key={item.id}
              item={item}
              names={names}
              onOpen={(opened) => setSelectedId(opened.id)}
              eager={index < 6}
            />
          ))}
        </ul>
      )}

      <div ref={sentinel} aria-hidden="true" />

      {(canLoadMore || moreError) && !loading && (
        <div className="mt-10 flex flex-col items-center gap-3">
          {moreError && <Notice tone="error">{moreError}</Notice>}
          <Button variant="secondary" onClick={() => void loadMore()} loading={loadingMore}>
            {loadingMore
              ? "Loading"
              : `Show more (${Math.max(0, total - (current?.offset ?? 0)).toLocaleString("en-US")} left)`}
          </Button>
        </div>
      )}

      <TitleDialog
        item={selected}
        names={names}
        onClose={() => setSelectedId(null)}
        actions={actions}
        readOnlyNote={readOnlyNote}
      />
    </div>
  );
}
