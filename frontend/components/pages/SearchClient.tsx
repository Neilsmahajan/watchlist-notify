"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import Poster from "@/components/Poster";
import { GRID_SIZES } from "@/components/watchlist/TitleTile";
import { Button, EmptyState, FIELD, Notice, PosterGridSkeleton, cx } from "@/components/ui";
import { IconCheck, IconPlus, IconSearch } from "@/components/icons";
import { redirectToLogin } from "@/lib/auth/client";
import { posterPathFromUrl } from "@/lib/tmdb";
import { readError } from "@/lib/watchlist/client";

type Kind = "movie" | "tv";

type Result = {
  tmdb_id?: number;
  title: string;
  year?: number;
  type?: Kind;
  poster_url?: string;
  poster_path?: string;
};

// What happened to a result the user tried to add.
type Added = "adding" | "added" | "exists";

const KINDS: { value: Kind; label: string }[] = [
  { value: "movie", label: "Films" },
  { value: "tv", label: "Series" },
];

const keyOf = (kind: Kind, result: Result) => `${kind}:${result.tmdb_id ?? result.title}`;

// Search TMDB and add what you find. The query and the kind live in the
// address bar, so a search can be reloaded, shared, or gone back to.
export default function SearchClient() {
  const params = useSearchParams();
  const query = (params.get("q") ?? "").trim().slice(0, 100);
  const kind: Kind = params.get("type") === "tv" ? "tv" : "movie";

  const [text, setText] = useState(query);
  const [results, setResults] = useState<Result[] | null>(null);
  const [shownFor, setShownFor] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [added, setAdded] = useState<Record<string, Added>>({});
  const [addError, setAddError] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);
  const input = useRef<HTMLInputElement>(null);

  const navigate = useCallback((nextQuery: string, nextKind: Kind, replace: boolean) => {
    const search = new URLSearchParams();
    if (nextQuery) search.set("q", nextQuery);
    if (nextKind === "tv") search.set("type", "tv");
    const url = search.size ? `/search?${search}` : "/search";
    if (replace) window.history.replaceState(null, "", url);
    else window.history.pushState(null, "", url);
  }, []);

  // Typing searches after a pause, without a history entry per keystroke.
  useEffect(() => {
    const wanted = text.trim();
    if (wanted === query) return;
    const timer = window.setTimeout(() => navigate(wanted, kind, true), 350);
    return () => window.clearTimeout(timer);
  }, [text, query, kind, navigate]);

  // The back button changes the address; the field follows.
  useEffect(() => {
    setText((current) => (current.trim() === query ? current : query));
  }, [query]);

  const requestKey = `${kind}:${query}`;
  useEffect(() => {
    if (!query) {
      setResults(null);
      setShownFor("");
      setError(null);
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    fetch(`/api/search?${new URLSearchParams({ query, type: kind })}`, {
      headers: { Accept: "application/json" },
      signal: controller.signal,
    })
      .then(async (response) => {
        if (response.status === 401) {
          redirectToLogin();
          return;
        }
        if (!response.ok) throw new Error(await readError(response, "The search did not work."));
        const data = (await response.json()) as { results?: Result[] };
        setResults(data.results ?? []);
        setShownFor(requestKey);
        setLoading(false);
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setError(cause instanceof Error ? cause.message : "The search did not work.");
        setLoading(false);
      });
    return () => controller.abort();
    // `requestKey` stands for query and kind.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [requestKey, retry]);

  const add = async (result: Result) => {
    const key = keyOf(kind, result);
    if (added[key]) return;
    setAdded((all) => ({ ...all, [key]: "adding" }));
    setAddError(null);
    try {
      const response = await fetch("/api/watchlist", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          title: result.title,
          type: kind === "tv" ? "show" : "movie",
          ...(typeof result.year === "number" && result.year > 0 ? { year: result.year } : {}),
          ...(typeof result.tmdb_id === "number" ? { tmdb_id: result.tmdb_id } : {}),
        }),
      });
      if (response.status === 401) return redirectToLogin();
      if (response.status === 409) {
        setAdded((all) => ({ ...all, [key]: "exists" }));
        return;
      }
      if (!response.ok) {
        throw new Error(await readError(response, `“${result.title}” could not be added.`));
      }
      setAdded((all) => ({ ...all, [key]: "added" }));
    } catch (cause) {
      setAdded((all) => {
        const next = { ...all };
        delete next[key];
        return next;
      });
      setAddError(cause instanceof Error ? cause.message : `“${result.title}” could not be added.`);
    }
  };

  const addedCount = Object.values(added).filter((state) => state === "added").length;
  const stale = loading || shownFor !== requestKey;

  return (
    <div>
      <form
        role="search"
        className="flex flex-col gap-3 sm:flex-row sm:items-center"
        onSubmit={(event) => {
          event.preventDefault();
          navigate(text.trim(), kind, false);
          input.current?.blur();
        }}
      >
        <div className="relative min-w-0 flex-1">
          <IconSearch className="pointer-events-none absolute left-4 top-1/2 size-5 -translate-y-1/2 text-faint" />
          <input
            ref={input}
            type="search"
            value={text}
            onChange={(event) => setText(event.target.value)}
            placeholder="Search for a film or series"
            aria-label="Search for a film or series"
            autoFocus
            autoComplete="off"
            enterKeyHint="search"
            maxLength={100}
            className={cx(FIELD, "h-14! rounded-2xl! pl-12 text-lg! sm:text-lg!")}
          />
        </div>
        <div role="radiogroup" aria-label="Kind" className="flex shrink-0 rounded-full border border-line-strong bg-surface p-1">
          {KINDS.map((option) => (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={kind === option.value}
              onClick={() => navigate(text.trim(), option.value, false)}
              className={cx(
                "h-10 flex-1 rounded-full px-5 text-sm font-medium transition-colors sm:flex-none",
                kind === option.value ? "bg-fg text-bg" : "text-muted hover:text-fg",
              )}
            >
              {option.label}
            </button>
          ))}
        </div>
      </form>

      {addedCount > 0 && (
        <Notice
          tone="ok"
          className="mt-6"
          action={
            <Link href="/watchlist" className="font-medium underline underline-offset-4">
              Open watchlist
            </Link>
          }
        >
          {addedCount === 1 ? "1 title added" : `${addedCount} titles added`} to your watchlist.
        </Notice>
      )}
      {addError && (
        <Notice tone="error" className="mt-6">
          {addError}
        </Notice>
      )}

      <div className="mt-8" aria-live="polite" aria-busy={loading}>
        {!query ? (
          <EmptyState title="What do you want to watch?">
            Type a title above. Each result can go onto your watchlist with one tap, and we
            start checking where it streams straight away.
          </EmptyState>
        ) : error ? (
          <Notice
            tone="error"
            action={
              <Button size="sm" variant="secondary" onClick={() => setRetry((n) => n + 1)}>
                Try again
              </Button>
            }
          >
            {error}
          </Notice>
        ) : results === null || (stale && results.length === 0) ? (
          <PosterGridSkeleton count={12} />
        ) : results.length === 0 ? (
          <EmptyState title={`Nothing found for “${query}”`}>
            Check the spelling, try fewer words, or switch between films and series.
          </EmptyState>
        ) : (
          <>
            <p className="slate mb-4 text-muted">
              {results.length} {results.length === 1 ? "result" : "results"} from TMDB
            </p>
            <ul className={cx("poster-grid transition-opacity duration-200", stale && "opacity-45")}>
              {results.map((result, index) => {
                const key = keyOf(kind, result);
                const state = added[key];
                const done = state === "added" || state === "exists";
                return (
                  <li key={`${key}:${index}`} className="flex min-w-0 flex-col">
                    <Poster
                      path={result.poster_path || posterPathFromUrl(result.poster_url)}
                      title={result.title}
                      year={result.year || undefined}
                      sizes={GRID_SIZES}
                      eager={index < 6}
                      decorative
                      className="ring-1 ring-line"
                    />
                    <p className="mt-2.5 text-[13px] font-medium leading-snug [overflow-wrap:anywhere] line-clamp-2 sm:text-sm">
                      {result.title}
                    </p>
                    <p className="slate mb-2.5 mt-1 text-faint">{result.year || "Year unknown"}</p>
                    <button
                      type="button"
                      onClick={() => void add(result)}
                      disabled={Boolean(state)}
                      aria-label={
                        done
                          ? `${result.title} is on your watchlist`
                          : `Add ${result.title} to your watchlist`
                      }
                      className={cx(
                        "mt-auto inline-flex h-9 w-full items-center justify-center gap-1.5 rounded-full text-[13px] font-medium transition-colors",
                        done
                          ? "bg-ok-soft text-ok"
                          : "bg-accent text-accent-ink hover:bg-accent-hover disabled:opacity-60",
                      )}
                    >
                      {done ? <IconCheck className="size-4" /> : <IconPlus className="size-4" />}
                      {state === "adding"
                        ? "Adding"
                        : state === "added"
                          ? "Added"
                          : state === "exists"
                            ? "On your list"
                            : "Add"}
                    </button>
                  </li>
                );
              })}
            </ul>
          </>
        )}
      </div>
    </div>
  );
}
