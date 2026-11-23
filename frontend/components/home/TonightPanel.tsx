"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import Poster from "@/components/Poster";
import ServiceLogo from "@/components/ServiceLogo";
import TitleDialog, { type DialogActions } from "@/components/watchlist/TitleDialog";
import { Button, FIELD, cx } from "@/components/ui";
import { formatRuntime } from "@/lib/format";
import { serviceName, type ServiceNames } from "@/lib/services";
import {
  MAX_EXCLUDE,
  pickTonight,
  type EmptyReason,
  type TonightOutcome,
  type TonightPick,
} from "@/lib/tonight";
import { recheckItem, removeItem, setItemStatus } from "@/lib/watchlist/client";

const SLOTS = 3;
const MAX_LOGOS = 3;
// With either mood chip in front, what is typed stays under the API's 200.
const TEXT_LIMIT = 140;
const SHORT_MINUTES = 120;

const MOODS = [
  { id: "light", label: "Something light" },
  { id: "tense", label: "Edge of my seat" },
] as const;
type MoodId = (typeof MOODS)[number]["id"];

// The results are three slots of a known size: stacked on a phone, side by
// side on a wide screen. The loading state draws the same three slots, so
// nothing on the page moves when the picks arrive.
const SLOT = "min-h-[10.5rem] sm:min-h-[9.75rem] xl:min-h-[13.5rem]";
// The note that stands in for missing picks fills the rest of the row on a
// wide screen. Stacked, it is only as tall as its text: an empty box the
// height of two cards would say less.
const NOTE = "xl:min-h-[13.5rem]";
const CARD = "flex w-full gap-3.5 rounded-2xl border border-line bg-surface p-3 sm:p-3.5 xl:p-4";
const POSTER = "block w-[72px] shrink-0 sm:w-[84px] xl:w-24";
const CHIP =
  "inline-flex h-[38px] items-center rounded-full border px-3.5 text-[13px] font-medium whitespace-nowrap transition-colors";

const CHIP_ON = "border-fg bg-fg text-bg";
const CHIP_OFF = "border-line-strong bg-surface text-fg hover:border-fg/40 hover:bg-surface-2";

const EMPTY_TITLES: Record<EmptyReason, string> = {
  no_services: "Choose your services first",
  nothing_streaming: "Nothing is streaming tonight",
  no_match: "Nothing fits that",
  exhausted: "You have seen them all",
};

type View =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "picks"; picks: TonightPick[] }
  | { kind: "empty"; reason: EmptyReason; message: string }
  | { kind: "limit"; scope: "user" | "total"; resetsAt: string | null }
  | { kind: "failed"; message: string; retry: boolean };

function changePicks(view: View, change: (picks: TonightPick[]) => TonightPick[]): View {
  if (view.kind !== "picks") return view;
  const picks = change(view.picks);
  return picks.length > 0 ? { kind: "picks", picks } : { kind: "idle" };
}

// When the daily limit resets, on the viewer's own clock. This is only ever
// rendered in the browser, after a request, so the local time zone is safe.
function resetText(resetsAt: string | null): string {
  if (!resetsAt) return "New picks tomorrow.";
  const at = new Date(resetsAt);
  const time = new Intl.DateTimeFormat(undefined, {
    hour: "numeric",
    minute: "2-digit",
    timeZoneName: "short",
  }).format(at);
  const today = at.toDateString() === new Date().toDateString();
  return `New picks ${today ? "today" : "tomorrow"} at ${time}.`;
}

function joinTitles(picks: TonightPick[]): string {
  const titles = picks.map((pick) => pick.item.title);
  if (titles.length < 2) return titles.join("");
  return `${titles.slice(0, -1).join(", ")} and ${titles[titles.length - 1]}`;
}

// "What should I watch tonight?" on the signed-in home page. It asks the API
// for three titles from the list that are streaming now on the viewer's
// services; Claude chooses them and says why. The home page leaves the panel
// out entirely when the API has the feature switched off.
export default function TonightPanel({ names }: { names: ServiceNames }) {
  const router = useRouter();
  const headingId = useId();
  const fieldId = useId();

  const [mood, setMood] = useState<MoodId | null>(null);
  const [short, setShort] = useState(false);
  const [text, setText] = useState("");
  const [view, setView] = useState<View>({ kind: "idle" });
  const [remaining, setRemaining] = useState<number | null>(null);
  // Titles already suggested, which "pick again" leaves out.
  const [shown, setShown] = useState<string[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const request = useRef<AbortController | null>(null);

  useEffect(() => () => request.current?.abort(), []);

  // The limit lifts by itself at the reset time.
  useEffect(() => {
    if (view.kind !== "limit" || !view.resetsAt) return;
    const wait = Date.parse(view.resetsAt) - Date.now() + 1000;
    const timer = setTimeout(
      () => {
        setView({ kind: "idle" });
        setRemaining(null);
      },
      Math.min(Math.max(wait, 0), 2 ** 31 - 1),
    );
    return () => clearTimeout(timer);
  }, [view]);

  const ask = async (exclude: string[], options: { short?: boolean } = {}) => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setView({ kind: "loading" });
    setSelectedId(null);

    const chip = MOODS.find((candidate) => candidate.id === mood)?.label;
    let outcome: TonightOutcome;
    try {
      outcome = await pickTonight(
        {
          mood: [chip, text.trim()].filter(Boolean).join(". "),
          maxMinutes: (options.short ?? short) ? SHORT_MINUTES : undefined,
          exclude,
        },
        controller.signal,
      );
    } catch {
      return; // cancelled by a newer request, or by leaving the page
    }
    if (controller.signal.aborted) return;

    switch (outcome.kind) {
      case "picks":
        setShown([...exclude, ...outcome.picks.map((pick) => pick.item.id)].slice(-MAX_EXCLUDE));
        setRemaining(outcome.remaining);
        setView({ kind: "picks", picks: outcome.picks.slice(0, SLOTS) });
        break;
      case "empty":
        setRemaining(outcome.remaining);
        setView({ kind: "empty", reason: outcome.reason, message: outcome.message });
        break;
      case "limit":
        setRemaining(0);
        setView(outcome);
        break;
      case "failed":
        // Whether a failed attempt was counted depends on where it failed.
        setRemaining(null);
        setView(outcome);
        break;
    }
  };

  const startOver = () => {
    setShown([]);
    void ask([]);
  };

  const anyLength = () => {
    setShort(false);
    void ask(shown, { short: false });
  };

  // The title dialog's actions, as on the shelves below: the change is made,
  // the card follows, and the server sends the page's counts again.
  const actions = useMemo<DialogActions>(
    () => ({
      async onStatus(item, status) {
        await setItemStatus(item, status);
        setView((current) =>
          changePicks(current, (picks) =>
            picks.map((pick) =>
              pick.item.id === item.id ? { ...pick, item: { ...pick.item, status } } : pick,
            ),
          ),
        );
        router.refresh();
      },
      async onRemove(item) {
        await removeItem(item);
        setSelectedId(null);
        setView((current) =>
          changePicks(current, (picks) => picks.filter((pick) => pick.item.id !== item.id)),
        );
        router.refresh();
      },
      async onRecheck(item) {
        const fresh = await recheckItem(item);
        if (fresh) {
          setView((current) =>
            changePicks(current, (picks) =>
              picks.map((pick) => (pick.item.id === fresh.id ? { ...pick, item: fresh } : pick)),
            ),
          );
        }
        router.refresh();
      },
    }),
    [router],
  );

  const loading = view.kind === "loading";
  const picks = view.kind === "picks" ? view.picks : [];
  const selected = picks.find((pick) => pick.item.id === selectedId)?.item ?? null;
  const missing = SLOTS - picks.length;

  let caption = "";
  let announcement = "";
  let body: ReactNode = null;

  switch (view.kind) {
    case "idle":
      break;

    case "loading":
      caption = "Choosing";
      announcement = "Choosing titles for tonight.";
      body = (
        <ul className="grid gap-3 xl:grid-cols-3" aria-hidden="true">
          {["", "[animation-delay:180ms]", "[animation-delay:360ms]"].map((delay) => (
            <li key={delay} className={cx(CARD, SLOT, "animate-shimmer", delay)}>
              <span className={cx(POSTER, "aspect-[2/3] rounded-lg bg-surface-2")} />
              <span className="block min-w-0 flex-1 pt-1">
                <span className="block h-4 w-3/5 rounded bg-surface-2" />
                <span className="mt-2.5 block h-2.5 w-2/5 rounded bg-surface-2" />
                <span className="mt-5 block h-2.5 w-full rounded bg-surface-2" />
                <span className="mt-2 block h-2.5 w-11/12 rounded bg-surface-2" />
                <span className="mt-2 block h-2.5 w-2/3 rounded bg-surface-2" />
              </span>
            </li>
          ))}
        </ul>
      );
      break;

    case "picks":
      caption = picks.length === 1 ? "Tonight's pick" : "Tonight's picks";
      announcement = `${picks.length === 1 ? "One pick" : `${picks.length} picks`} for tonight: ${joinTitles(picks)}.`;
      body = (
        <ul className="grid gap-3 xl:grid-cols-3">
          {picks.map((pick) => (
            <PickCard key={pick.item.id} pick={pick} names={names} onOpen={setSelectedId} />
          ))}
          {missing > 0 && (
            <li
              className={cx(
                "flex items-center rounded-2xl border border-dashed border-line-strong p-5 text-sm leading-relaxed text-muted",
                NOTE,
                missing === 2 && "xl:col-span-2",
              )}
            >
              <p className="max-w-md">
                That is everything that fits right now. Change the mood, or{" "}
                <Link href="/search" className="text-accent-text underline underline-offset-4">
                  add more titles
                </Link>{" "}
                to your list.
              </p>
            </li>
          )}
        </ul>
      );
      break;

    case "empty": {
      const title = EMPTY_TITLES[view.reason];
      announcement = `${title}. ${view.message}`;
      body = (
        <Message
          title={title}
          action={
            <>
              {view.reason === "no_services" && (
                <Button href="/settings#services">Choose services</Button>
              )}
              {view.reason === "nothing_streaming" && (
                <>
                  <Button href="/settings#services" variant="secondary">
                    Edit services
                  </Button>
                  <Button href="/search" variant="secondary">
                    Add titles
                  </Button>
                </>
              )}
              {view.reason === "no_match" && short && (
                <Button variant="secondary" onClick={anyLength}>
                  Try any length
                </Button>
              )}
              {view.reason === "exhausted" && (
                <Button variant="secondary" onClick={startOver}>
                  Start over
                </Button>
              )}
            </>
          }
        >
          {view.message}
        </Message>
      );
      break;
    }

    case "limit": {
      const title =
        view.scope === "total" ? "Today's picks are all used" : "You have used today's picks";
      const detail = `${
        view.scope === "total" ? "The daily limit is shared by everyone here. " : ""
      }${resetText(view.resetsAt)}`;
      announcement = `${title}. ${detail}`;
      body = <Message title={title}>{detail}</Message>;
      break;
    }

    case "failed":
      // The message is an alert, which is announced by itself.
      body = (
        <Message
          alert
          title="That did not work"
          action={
            view.retry && (
              <Button variant="secondary" onClick={() => void ask(shown)}>
                Try again
              </Button>
            )
          }
        >
          {view.message}
          {view.retry && " Nothing on your list was changed."}
        </Message>
      );
      break;
  }

  return (
    <section
      aria-labelledby={headingId}
      className="relative mt-10 overflow-hidden rounded-3xl border border-line bg-surface sm:mt-12"
    >
      {/* The marquee light along the top edge. */}
      <span
        aria-hidden="true"
        className="absolute inset-x-10 top-0 h-px bg-gradient-to-r from-transparent via-accent to-transparent"
      />

      <div className="grid gap-x-14 gap-y-6 px-4 pb-5 pt-6 sm:p-7 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1fr)] xl:items-end xl:p-9">
        <div>
          <p className="eyebrow">Tonight</p>
          <h2 id={headingId} className="display mt-3 text-3xl sm:text-4xl">
            What should I watch tonight?
          </h2>
          <p className="mt-3 max-w-md text-sm leading-relaxed text-muted">
            Three picks from your list, chosen from what is streaming on your
            services right now.
          </p>
        </div>

        <form
          className="min-w-0"
          onSubmit={(event) => {
            event.preventDefault();
            if (!loading && view.kind !== "limit") void ask(shown);
          }}
        >
          <div role="group" aria-label="Mood" className="flex flex-wrap gap-2">
            {MOODS.map((option) => (
              <button
                key={option.id}
                type="button"
                aria-pressed={mood === option.id}
                onClick={() => setMood(mood === option.id ? null : option.id)}
                className={cx(CHIP, mood === option.id ? CHIP_ON : CHIP_OFF)}
              >
                {option.label}
              </button>
            ))}
            <button
              type="button"
              aria-pressed={short}
              onClick={() => setShort(!short)}
              className={cx(CHIP, short ? CHIP_ON : CHIP_OFF)}
            >
              Short, under 2 hours
            </button>
          </div>

          <div className="mt-3 flex flex-col gap-2 sm:flex-row">
            <label htmlFor={fieldId} className="sr-only">
              What you are in the mood for, in your own words (optional)
            </label>
            <input
              id={fieldId}
              type="text"
              value={text}
              onChange={(event) => setText(event.target.value)}
              maxLength={TEXT_LIMIT}
              placeholder="Or in your own words"
              autoComplete="off"
              enterKeyHint="go"
              className={cx(FIELD, "rounded-full! px-5!")}
            />
            <Button
              type="submit"
              loading={loading}
              disabled={view.kind === "limit"}
              className="h-11! shrink-0 sm:w-52"
            >
              {shown.length > 0 ? "Pick again" : "Pick three for tonight"}
            </Button>
          </div>

          <p className="mt-3 text-xs leading-relaxed text-muted">
            The titles on your list and what you type here are sent to
            Anthropic&apos;s Claude to make the pick.{" "}
            <Link href="/privacy#ai" className="underline underline-offset-2 hover:text-fg">
              Privacy
            </Link>
          </p>
        </form>
      </div>

      {view.kind !== "idle" && (
        <div className="bg-surface-2/40">
          <div className="h-0.5 overflow-hidden bg-line" aria-hidden="true">
            {loading && (
              <div className="h-full w-1/3 animate-[slide_1.4s_ease-in-out_infinite] bg-accent" />
            )}
          </div>
          <div className="px-4 pb-4 pt-3 sm:px-7 sm:pb-7 sm:pt-4 xl:px-9 xl:pb-9">
            <div className="mb-3 flex h-5 items-center justify-between gap-4">
              <p className="slate truncate text-muted">{caption}</p>
              {remaining !== null && (
                <p className="slate shrink-0 text-muted">
                  {remaining === 0
                    ? "No picks left today"
                    : `${remaining} ${remaining === 1 ? "pick" : "picks"} left today`}
                </p>
              )}
            </div>
            {body}
          </div>
        </div>
      )}

      <p role="status" className="sr-only">
        {announcement}
      </p>

      <TitleDialog
        item={selected}
        names={names}
        onClose={() => setSelectedId(null)}
        actions={actions}
      />
    </section>
  );
}

// One pick: the poster, the title, where it is streaming and why tonight.
// The whole card is one button that opens the title's detail view.
function PickCard({
  pick,
  names,
  onOpen,
}: {
  pick: TonightPick;
  names: ServiceNames;
  onOpen: (id: string) => void;
}) {
  const { item, reason } = pick;
  const providers = item.availability?.providers ?? [];
  const providerNames = providers.map((p) => serviceName(p.code, names, p.name));
  const slate = [item.year, formatRuntime(item.runtime)].filter(Boolean).join(" · ");

  return (
    <li className="min-w-0 animate-fade">
      <button
        type="button"
        onClick={() => onOpen(item.id)}
        aria-haspopup="dialog"
        className={cx(
          CARD,
          SLOT,
          "group h-full text-left transition-[border-color,box-shadow,transform] duration-200 hover:-translate-y-0.5 hover:border-line-strong hover:shadow-lift",
        )}
      >
        <span className={POSTER}>
          <Poster
            path={item.poster_path}
            title={item.title}
            year={item.year}
            sizes="96px"
            eager
            decorative
            className="ring-1 ring-line"
          />
        </span>
        <span className="block min-w-0 flex-1">
          <span className="block font-medium leading-snug [overflow-wrap:anywhere] line-clamp-2">
            {item.title}
          </span>
          <span className="mt-1.5 flex min-h-6 items-center justify-between gap-3">
            <span className="slate text-faint">{slate}</span>
            <span className="flex shrink-0 items-center gap-1">
              {providers.slice(0, MAX_LOGOS).map((provider, index) => (
                <ServiceLogo
                  key={provider.code ?? provider.name}
                  code={provider.code}
                  name={providerNames[index]}
                  logoPath={provider.logo_path}
                  labelled
                  className="size-6"
                />
              ))}
              {providers.length > MAX_LOGOS && (
                <span className="slate text-faint">+{providers.length - MAX_LOGOS}</span>
              )}
            </span>
          </span>
          <span className="mt-2 block text-[13px] leading-relaxed text-muted [overflow-wrap:anywhere] xl:text-sm xl:leading-relaxed">
            {reason}
          </span>
          {providerNames.length > 0 && (
            <span className="sr-only">{` Streaming on ${providerNames.join(", ")}.`}</span>
          )}
        </span>
      </button>
    </li>
  );
}

// What the results area says when there are no picks to show.
function Message({
  title,
  children,
  action,
  alert = false,
}: {
  title: string;
  children: ReactNode;
  action?: ReactNode;
  alert?: boolean;
}) {
  return (
    <div
      role={alert ? "alert" : undefined}
      className={cx(
        "flex flex-col items-center justify-center rounded-2xl border border-dashed border-line-strong px-5 py-7 text-center",
        SLOT,
      )}
    >
      <h3 className="display text-xl sm:text-2xl">{title}</h3>
      <p className="mt-2 max-w-md text-sm leading-relaxed text-muted">{children}</p>
      {action && <div className="mt-5 flex flex-wrap justify-center gap-2 empty:hidden">{action}</div>}
    </div>
  );
}
