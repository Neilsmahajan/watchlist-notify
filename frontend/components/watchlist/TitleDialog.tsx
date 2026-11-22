"use client";

import { useEffect, useRef, useState } from "react";
import Poster from "@/components/Poster";
import ServiceLogo from "@/components/ServiceLogo";
import { Button, Notice, cx } from "@/components/ui";
import { IconClose, IconExternal, IconRefresh, IconStar, IconTrash } from "@/components/icons";
import { formatDate, formatRating, formatRuntime } from "@/lib/format";
import { accessLabel, serviceName, type ServiceNames } from "@/lib/services";
import {
  STATUS_LABELS,
  TYPE_LABELS,
  type Provider,
  type WatchlistItem,
  type WatchlistStatus,
} from "@/lib/watchlist/types";

const STATUSES: WatchlistStatus[] = ["planned", "watching", "finished"];
const STREAMING = ["subscription", "free", "ads"];

export type DialogActions = {
  onStatus: (item: WatchlistItem, status: WatchlistStatus) => Promise<void>;
  onRemove: (item: WatchlistItem) => Promise<void>;
  onRecheck: (item: WatchlistItem) => Promise<void>;
};

type TitleDialogProps = {
  item: WatchlistItem | null;
  names: ServiceNames;
  onClose: () => void;
  // Absent in the demo, which is read-only.
  actions?: DialogActions;
  // Shown in place of the actions when there are none.
  readOnlyNote?: React.ReactNode;
};

// The detail view: a centred dialog on a wide screen, a sheet rising from
// the bottom on a phone. A native <dialog>, so focus is trapped, Escape
// closes it and focus returns to the tile that opened it.
export default function TitleDialog({
  item,
  names,
  onClose,
  actions,
  readOnlyNote,
}: TitleDialogProps) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    if (item && !dialog.open) {
      dialog.showModal();
    } else if (!item && dialog.open) {
      dialog.close();
    }
  }, [item]);

  // Keep the page behind from scrolling while the dialog is open.
  useEffect(() => {
    if (!item) return;
    const root = document.documentElement;
    const previous = root.style.overflow;
    root.style.overflow = "hidden";
    return () => {
      root.style.overflow = previous;
    };
  }, [item]);

  return (
    <dialog
      ref={ref}
      onClose={onClose}
      onClick={(event) => {
        // A click on the backdrop lands on the dialog element itself.
        if (event.target === ref.current) ref.current?.close();
      }}
      aria-labelledby="title-dialog-heading"
      className="m-0 mt-auto max-h-[92dvh] w-full max-w-none overflow-hidden rounded-t-3xl border border-line bg-surface p-0 shadow-pop open:flex open:animate-rise sm:m-auto sm:max-h-[min(88dvh,760px)] sm:max-w-3xl sm:rounded-3xl"
    >
      {item && (
        <DialogBody
          key={item.id}
          item={item}
          names={names}
          actions={actions}
          readOnlyNote={readOnlyNote}
          onClose={() => ref.current?.close()}
        />
      )}
    </dialog>
  );
}

function DialogBody({
  item,
  names,
  actions,
  readOnlyNote,
  onClose,
}: {
  item: WatchlistItem;
  names: ServiceNames;
  actions?: DialogActions;
  readOnlyNote?: React.ReactNode;
  onClose: () => void;
}) {
  const [busy, setBusy] = useState<"status" | "remove" | "recheck" | null>(null);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = async (kind: "status" | "remove" | "recheck", work: () => Promise<void>) => {
    setBusy(kind);
    setError(null);
    try {
      await work();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "That did not work. Try again.");
    } finally {
      setBusy(null);
    }
  };

  const availability = item.availability;
  const mine = availability?.providers ?? [];
  const elsewhere = availability?.elsewhere ?? [];
  const alsoStreaming = elsewhere.filter((p) => (p.access ?? []).some((a) => STREAMING.includes(a)));
  const stores = elsewhere.filter((p) => !(p.access ?? []).some((a) => STREAMING.includes(a)));

  const runtime = formatRuntime(item.runtime);
  const rating = formatRating(item.rating);
  const added = formatDate(item.added_at);
  const checked = formatDate(availability?.checked_at);
  const tmdbKind = item.type === "movie" ? "movie" : "tv";

  return (
    <div className="flex min-h-0 w-full flex-col sm:flex-row">
      <button
        type="button"
        onClick={onClose}
        aria-label="Close"
        className="absolute right-3 top-3 z-10 inline-flex size-9 items-center justify-center rounded-full bg-surface-2 text-muted transition-colors hover:text-fg"
      >
        <IconClose className="size-[18px]" />
      </button>

      <div className="hidden w-[260px] shrink-0 bg-surface-2 p-6 sm:block">
        <Poster
          path={item.poster_path}
          title={item.title}
          year={item.year}
          sizes="212px"
          eager
          decorative
          className="shadow-lift ring-1 ring-line"
        />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-[max(1.5rem,env(safe-area-inset-bottom))] pt-5 sm:px-7 sm:pb-7 sm:pt-7">
        <div className="flex gap-4 pr-10">
          <div className="w-20 shrink-0 sm:hidden">
            <Poster
              path={item.poster_path}
              title={item.title}
              year={item.year}
              sizes="80px"
              eager
              decorative
              className="ring-1 ring-line"
            />
          </div>
          <div className="min-w-0">
            <p className="eyebrow">{TYPE_LABELS[item.type] ?? "Title"}</p>
            <h2
              id="title-dialog-heading"
              className="display mt-1.5 text-[1.7rem] [overflow-wrap:anywhere] sm:text-4xl"
            >
              {item.title}
            </h2>
            <p className="slate mt-2.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-muted">
              {item.year ? <span>{item.year}</span> : null}
              {runtime && <span>{runtime}</span>}
              {rating && (
                <span className="inline-flex items-center gap-1" title="TMDB rating">
                  <IconStar className="size-3 text-accent-text" />
                  {rating}
                  <span className="sr-only">out of 10 on TMDB</span>
                </span>
              )}
            </p>
          </div>
        </div>

        {item.genres && item.genres.length > 0 && (
          <ul className="mt-4 flex flex-wrap gap-1.5">
            {item.genres.map((genre) => (
              <li key={genre} className="rounded-full border border-line px-2.5 py-0.5 text-xs text-muted">
                {genre}
              </li>
            ))}
          </ul>
        )}

        {item.overview && (
          <p className="mt-4 text-[15px] leading-relaxed text-muted">{item.overview}</p>
        )}

        <section className="mt-6" aria-labelledby="where-heading">
          <h3 id="where-heading" className="slate text-faint">
            On your services
          </h3>
          {!availability ? (
            <p className="mt-2 text-sm text-muted">
              Availability has not been checked for this title yet.
            </p>
          ) : mine.length > 0 ? (
            <ProviderList providers={mine} names={names} highlight />
          ) : (
            <p className="mt-2 text-sm text-muted">
              Not streaming on your services right now.
            </p>
          )}
        </section>

        {alsoStreaming.length > 0 && (
          <section className="mt-5" aria-labelledby="also-heading">
            <h3 id="also-heading" className="slate text-faint">
              Also streaming on
            </h3>
            <ProviderList providers={alsoStreaming} names={names} />
          </section>
        )}

        {stores.length > 0 && (
          <section className="mt-5" aria-labelledby="stores-heading">
            <h3 id="stores-heading" className="slate text-faint">
              Rent or buy
            </h3>
            <ProviderList providers={stores} names={names} />
          </section>
        )}

        <p className="mt-5 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-faint">
          {availability?.link && (
            <a
              href={availability.link}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 font-medium text-accent-text hover:underline"
            >
              All watch options
              <IconExternal className="size-3.5" />
            </a>
          )}
          {item.tmdb_id ? (
            <a
              href={`https://www.themoviedb.org/${tmdbKind}/${item.tmdb_id}`}
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-fg hover:underline"
            >
              TMDB
            </a>
          ) : null}
          {item.imdb_id && (
            <a
              href={`https://www.imdb.com/title/${item.imdb_id}/`}
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-fg hover:underline"
            >
              IMDb
            </a>
          )}
          {availability && (
            <span>
              Availability by JustWatch{checked ? `, checked ${checked}` : ""}
              {availability.region ? ` (${availability.region})` : ""}
            </span>
          )}
        </p>

        <div className="mt-6 border-t border-line pt-5">
          {error && (
            <Notice tone="error" className="mb-4">
              {error}
            </Notice>
          )}

          {actions ? (
            <>
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div
                  role="radiogroup"
                  aria-label="Status"
                  className="inline-flex rounded-full border border-line bg-surface-2 p-0.5"
                >
                  {STATUSES.map((status) => {
                    const selected = item.status === status;
                    return (
                      <button
                        key={status}
                        type="button"
                        role="radio"
                        aria-checked={selected}
                        disabled={busy !== null}
                        onClick={() =>
                          !selected && run("status", () => actions.onStatus(item, status))
                        }
                        className={cx(
                          "rounded-full px-3.5 py-1.5 text-sm transition-colors",
                          selected
                            ? "bg-fg font-medium text-bg"
                            : "text-muted hover:text-fg",
                        )}
                      >
                        {STATUS_LABELS[status]}
                      </button>
                    );
                  })}
                </div>

                {confirmRemove ? (
                  <span className="flex items-center gap-2 text-sm">
                    <span className="text-muted">Remove it?</span>
                    <Button
                      size="sm"
                      variant="danger"
                      loading={busy === "remove"}
                      onClick={() => run("remove", () => actions.onRemove(item))}
                    >
                      Remove
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setConfirmRemove(false)}>
                      Keep
                    </Button>
                  </span>
                ) : (
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={busy !== null}
                    onClick={() => setConfirmRemove(true)}
                  >
                    <IconTrash className="size-4" />
                    Remove
                  </Button>
                )}
              </div>

              <div className="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-faint">
                <span>
                  {added ? `Added ${added}` : ""}
                  {item.source === "imdb" ? " from your IMDb list" : ""}
                  {item.source === "csv" ? " from a CSV import" : ""}
                  {item.imdb_missing_since && (
                    <span
                      className="block pt-0.5"
                      title="It will be removed at a later sync unless you change its status here."
                    >
                      No longer on your IMDb list
                    </span>
                  )}
                </span>
                {item.tmdb_id ? (
                  <button
                    type="button"
                    disabled={busy !== null}
                    onClick={() => run("recheck", () => actions.onRecheck(item))}
                    className="inline-flex items-center gap-1.5 rounded-full px-2 py-1 text-muted transition-colors hover:text-fg disabled:opacity-50"
                  >
                    <IconRefresh className={cx("size-3.5", busy === "recheck" && "animate-spin")} />
                    {busy === "recheck" ? "Checking" : "Check availability again"}
                  </button>
                ) : null}
              </div>
            </>
          ) : (
            // Wrapped so the note, which arrives from a server component, is
            // an only child rather than one of a list of siblings.
            <div>{readOnlyNote}</div>
          )}
        </div>
      </div>
    </div>
  );
}

function ProviderList({
  providers,
  names,
  highlight = false,
}: {
  providers: Provider[];
  names: ServiceNames;
  highlight?: boolean;
}) {
  return (
    <ul className="mt-2.5 flex flex-wrap gap-2">
      {providers.map((provider) => {
        const name = serviceName(provider.code, names, provider.name);
        const access = (provider.access ?? []).map(accessLabel).join(" · ");
        return (
          <li
            key={provider.code ?? provider.name}
            className={cx(
              "flex items-center gap-2.5 rounded-xl border py-1.5 pl-1.5 pr-3.5",
              highlight ? "border-accent/50 bg-accent-soft" : "border-line",
            )}
          >
            <ServiceLogo
              code={provider.code}
              name={name}
              logoPath={provider.logo_path}
              labelled
              className="size-8"
            />
            <span className="min-w-0 leading-tight">
              <span className="block text-sm font-medium">{name}</span>
              {access && <span className="block text-[11px] text-muted">{access}</span>}
            </span>
          </li>
        );
      })}
    </ul>
  );
}
