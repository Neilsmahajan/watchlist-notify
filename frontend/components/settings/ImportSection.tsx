"use client";

import Link from "next/link";
import { useEffect, useId, useRef, useState } from "react";
import { Button, FIELD, Notice, cx } from "@/components/ui";
import { IconExternal, IconUpload } from "@/components/icons";
import { redirectToLogin } from "@/lib/auth/client";
import { formatDateTime } from "@/lib/format";
import { readError } from "@/lib/watchlist/client";
import type { ImdbSync } from "@/lib/watchlist/types";

type LinkResult = {
  total?: number;
  added?: number;
  existing?: number;
  unmatched?: number;
  complete?: boolean;
};

type Message = { tone: "ok" | "error" | "info"; text: string };

const STATUS: Record<NonNullable<ImdbSync["last_status"]>, { label: string; tone: string }> = {
  ok: { label: "In sync", tone: "bg-ok-soft text-ok" },
  partial: { label: "Partly synced", tone: "bg-accent-soft text-accent-text" },
  failed: { label: "Last sync failed", tone: "bg-danger-soft text-danger" },
  not_found: { label: "List not found or private", tone: "bg-danger-soft text-danger" },
};

// Linking reads the whole list on IMDb before it answers. What to say while
// that happens, by seconds waited.
const PROGRESS: [number, string][] = [
  [0, "Reading your list on IMDb"],
  [5, "Matching titles"],
  [12, "Adding them to your watchlist"],
  [20, "Still going: long lists take a little longer"],
];

function imdbHref(sourceId: string): string {
  return sourceId.startsWith("ls")
    ? `https://www.imdb.com/list/${encodeURIComponent(sourceId)}/`
    : `https://www.imdb.com/user/${encodeURIComponent(sourceId)}/watchlist`;
}

function Count({ label, value }: { label: string; value?: number }) {
  return (
    <div>
      <dt className="slate text-muted">{label}</dt>
      <dd className="display mt-1 text-3xl">{(value ?? 0).toLocaleString("en-US")}</dd>
    </div>
  );
}

function ImdbLink({ initial }: { initial: ImdbSync | null }) {
  const [sync, setSync] = useState(initial);
  const [source, setSource] = useState("");
  const [busy, setBusy] = useState<"link" | "unlink" | null>(null);
  const [seconds, setSeconds] = useState(0);
  const [message, setMessage] = useState<Message | null>(null);
  const [result, setResult] = useState<LinkResult | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  const [confirmUnlink, setConfirmUnlink] = useState(false);
  // The API allows one attempt a minute, failed ones included. Seconds left.
  const [cooldown, setCooldown] = useState(0);
  // State updates are not instant; this is what stops a second submit.
  const inFlight = useRef(false);
  const fieldId = useId();

  const waiting = cooldown > 0;
  useEffect(() => {
    if (!waiting) return;
    const timer = window.setInterval(() => setCooldown((n) => Math.max(0, n - 1)), 1000);
    return () => window.clearInterval(timer);
  }, [waiting]);

  useEffect(() => {
    if (busy !== "link") return;
    setSeconds(0);
    const timer = window.setInterval(() => setSeconds((n) => n + 1), 1000);
    return () => window.clearInterval(timer);
  }, [busy]);

  const link = async (value: string) => {
    const wanted = value.trim();
    if (!wanted) {
      setMessage({ tone: "error", text: "Enter your IMDb user ID, a list ID, or the list's address." });
      return;
    }
    if (inFlight.current || cooldown > 0) return;
    inFlight.current = true;
    setBusy("link");
    setMessage(null);
    setResult(null);
    try {
      const response = await fetch("/api/me/imdb", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ source: wanted }),
      });
      if (response.status === 401) return redirectToLogin();
      const body = (await response.json().catch(() => null)) as {
        error?: string;
        imdb_sync?: ImdbSync;
        result?: LinkResult;
      } | null;

      if (!response.ok) {
        // The API names the problem when the list is the problem. A 404 or
        // 405 without one means this server has no IMDb linking yet.
        if ((response.status === 404 || response.status === 405) && !body?.error) {
          setUnavailable(true);
          return;
        }
        const fallback: Record<number, string> = {
          400: "That does not look like an IMDb user ID, list ID or list address.",
          404: "IMDb has no public list at that address. Check that the list is set to public.",
          429: "Too soon after the last attempt. Try again in a minute.",
          500: "The link could not be saved. Try again in a minute.",
          502: "IMDb could not be reached. Try again in a minute.",
          503: "Linking is not available right now. Try again later.",
        };
        // Every attempt that reached IMDb counts towards the limit, so the
        // next one would be refused: say when it can be made instead.
        if ([404, 429, 502].includes(response.status)) {
          const wait = Number.parseInt(response.headers.get("Retry-After") ?? "", 10);
          setCooldown(Number.isFinite(wait) && wait > 0 ? Math.min(wait, 600) : 60);
        }
        setMessage({
          tone: "error",
          text:
            (body?.error || fallback[response.status] || "The list could not be linked.") +
            (sync ? " Your current link is unchanged." : ""),
        });
        return;
      }

      setSync(body?.imdb_sync ?? null);
      setResult(body?.result ?? {});
      setSource("");
      setCooldown(60);
    } catch {
      setMessage({ tone: "error", text: "The list could not be linked. Check your connection and try again." });
    } finally {
      inFlight.current = false;
      setBusy(null);
    }
  };

  const unlink = async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy("unlink");
    setMessage(null);
    try {
      const response = await fetch("/api/me/imdb", { method: "DELETE" });
      if (response.status === 401) return redirectToLogin();
      if (!response.ok) throw new Error(await readError(response, "The list could not be unlinked."));
      setSync(null);
      setResult(null);
      setConfirmUnlink(false);
      setMessage({ tone: "ok", text: "Unlinked. Every title stays on your watchlist." });
    } catch (cause) {
      setMessage({
        tone: "error",
        text: cause instanceof Error ? cause.message : "The list could not be unlinked.",
      });
    } finally {
      inFlight.current = false;
      setBusy(null);
    }
  };

  const progress = [...PROGRESS].reverse().find(([from]) => seconds >= from)?.[1];
  const status = sync?.last_status ? STATUS[sync.last_status] : null;
  const lastSynced = formatDateTime(sync?.last_synced_at);
  const lastAttempt = formatDateTime(sync?.last_attempt_at);

  return (
    <div>
      <h3 className="text-base font-semibold">Link an IMDb watchlist</h3>
      <p className="mt-1.5 text-sm leading-relaxed text-muted">
        Keep adding titles on IMDb and they appear here. The list must be set to public on
        IMDb. It is read once a day.
      </p>

      {unavailable ? (
        <Notice tone="info" className="mt-4">
          Linking an IMDb list is not available yet. It is on its way; until then, the CSV
          import below does the same job once.
        </Notice>
      ) : (
        <>
          {sync && (
            <div className="mt-4 rounded-2xl border border-line bg-surface p-4 sm:p-5">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                <a
                  href={imdbHref(sync.source_id)}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1.5 font-mono text-sm underline-offset-4 hover:underline"
                >
                  {sync.source_id}
                  <IconExternal className="size-3.5 text-muted" />
                </a>
                {status && (
                  <span className={cx("slate rounded-full px-2.5 py-1 font-medium", status.tone)}>
                    {status.label}
                  </span>
                )}
              </div>
              <dl className="mt-5 grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-4">
                <Count label="On IMDb" value={sync.total} />
                <Count label="Added here" value={sync.added} />
                <Count label="Removed" value={sync.removed} />
                <Count label="No match" value={sync.unmatched} />
              </dl>
              <p className="slate mt-5 border-t border-line pt-4 text-muted">
                {lastSynced
                  ? `Last synced ${lastSynced}`
                  : lastAttempt
                    ? `Last tried ${lastAttempt}`
                    : "Not synced yet"}
              </p>
              {sync.last_error && sync.last_status !== "ok" && (
                <p className="mt-2 text-sm text-danger">{sync.last_error}</p>
              )}
              <div className="mt-4 flex flex-wrap items-center gap-2.5">
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => void link(sync.source_id)}
                  loading={busy === "link"}
                  disabled={busy !== null || waiting}
                >
                  {waiting ? `Sync again in ${cooldown}s` : "Sync now"}
                </Button>
                {confirmUnlink ? (
                  <>
                    <Button
                      size="sm"
                      variant="danger"
                      onClick={() => void unlink()}
                      loading={busy === "unlink"}
                      disabled={busy !== null}
                    >
                      Yes, unlink
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setConfirmUnlink(false)}>
                      Keep it
                    </Button>
                  </>
                ) : (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setConfirmUnlink(true)}
                    disabled={busy !== null}
                  >
                    Unlink
                  </Button>
                )}
              </div>
            </div>
          )}

          <form
            className="mt-4"
            onSubmit={(event) => {
              event.preventDefault();
              void link(source);
            }}
          >
            <label htmlFor={fieldId} className="text-sm font-medium">
              {sync ? "Link a different list" : "Your IMDb user ID, list ID or list address"}
            </label>
            <div className="mt-2 flex flex-col gap-2.5 sm:flex-row">
              <input
                id={fieldId}
                type="text"
                value={source}
                onChange={(event) => setSource(event.target.value)}
                placeholder="ur12345678 or imdb.com/user/ur12345678/watchlist"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                disabled={busy !== null}
                className={cx(FIELD, "font-mono sm:text-sm!")}
              />
              <Button
                type="submit"
                loading={busy === "link"}
                disabled={busy !== null || waiting}
                className="shrink-0"
              >
                {busy === "link"
                  ? "Linking"
                  : waiting
                    ? `${message?.tone === "error" ? "Try again" : "Ready"} in ${cooldown}s`
                    : sync
                      ? "Replace"
                      : "Link list"}
              </Button>
            </div>
          </form>

          {busy === "link" && (
            <div className="mt-4" role="status" aria-live="polite">
              <div className="h-1 overflow-hidden rounded-full bg-surface-2">
                <div className="h-full w-1/3 animate-[slide_1.4s_ease-in-out_infinite] rounded-full bg-accent" />
              </div>
              <p className="mt-2 text-sm text-muted">
                {progress}. This can take about 20 seconds; keep this page open.
              </p>
            </div>
          )}

          {result && busy === null && (
            <Notice
              tone="ok"
              className="mt-4"
              action={
                <Link href="/watchlist" className="font-medium underline underline-offset-4">
                  Open watchlist
                </Link>
              }
            >
              Linked. {(result.total ?? 0).toLocaleString("en-US")} titles on the list:{" "}
              {(result.added ?? 0).toLocaleString("en-US")} added,{" "}
              {(result.existing ?? 0).toLocaleString("en-US")} already here
              {result.unmatched ? `, ${result.unmatched.toLocaleString("en-US")} could not be matched` : ""}.
              {(result.complete === false || sync?.last_status === "partial") &&
                " The list was only partly read; the rest will be picked up by tomorrow's sync."}
            </Notice>
          )}
        </>
      )}

      {message && (
        <Notice tone={message.tone} className="mt-4">
          {message.text}
        </Notice>
      )}

      <ul className="mt-5 list-disc space-y-1.5 pl-5 text-sm leading-relaxed text-muted">
        <li>
          A title you take off the IMDb list is removed here after two daily syncs, unless you
          changed its status here: titles you marked as watching or finished stay.
        </li>
        <li>Unlinking stops the syncing and keeps every title already on your watchlist.</li>
      </ul>
    </div>
  );
}

// The API reports rows it could not import, and may add summary rows.
type CsvRow = { row?: number; reason?: string; [key: string]: unknown };

type CsvResult = {
  imported?: number;
  duplicates?: number;
  errors?: CsvRow[] | null;
};

function describeRow(problem: CsvRow): string {
  const reason =
    typeof problem.reason === "string" && problem.reason
      ? problem.reason
      : JSON.stringify(problem);
  return typeof problem.row === "number" && problem.row > 0
    ? `Row ${problem.row}: ${reason}`
    : reason;
}

function CsvImport() {
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<CsvResult | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const inFlight = useRef(false);

  const upload = async () => {
    if (!file || inFlight.current) return;
    if (file.size > 10 * 1024 * 1024) {
      setError("That file is larger than 10 MB.");
      return;
    }
    inFlight.current = true;
    setBusy(true);
    setError(null);
    setResult(null);
    try {
      const form = new FormData();
      form.set("file", file);
      const response = await fetch("/api/watchlist/import", { method: "POST", body: form });
      if (response.status === 401) return redirectToLogin();
      if (!response.ok) throw new Error(await readError(response, "The file could not be imported."));
      setResult((await response.json()) as CsvResult);
      setFile(null);
      if (input.current) input.current.value = "";
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "The file could not be imported.");
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  };

  const problems = result?.errors ?? [];

  return (
    <div>
      <h3 className="text-base font-semibold">Import a CSV file</h3>
      <p className="mt-1.5 text-sm leading-relaxed text-muted">
        A one-off import. On IMDb, open your watchlist or any list and choose Export; the
        file you get works as it is. Titles already on your watchlist are skipped.
      </p>

      <div className="mt-4 flex flex-col gap-2.5 sm:flex-row sm:items-center">
        <label
          className={cx(
            "flex min-w-0 flex-1 cursor-pointer items-center gap-3 rounded-xl border border-dashed border-line-strong bg-surface px-4 py-2.5 text-sm transition-colors hover:border-fg/40",
            busy && "pointer-events-none opacity-60",
          )}
        >
          <IconUpload className="size-4 shrink-0 text-muted" />
          <span className={cx("truncate", !file && "text-muted")}>
            {file ? file.name : "Choose a .csv file"}
          </span>
          <input
            ref={input}
            type="file"
            accept=".csv,text/csv"
            className="sr-only"
            onChange={(event) => {
              setFile(event.target.files?.[0] ?? null);
              setError(null);
              setResult(null);
            }}
          />
        </label>
        <Button onClick={() => void upload()} loading={busy} disabled={!file} className="shrink-0">
          {busy ? "Importing" : "Import"}
        </Button>
      </div>
      {busy && (
        <p className="mt-2 text-sm text-muted" role="status">
          Matching each row. A long list can take up to a minute.
        </p>
      )}

      {error && (
        <Notice tone="error" className="mt-4">
          {error}
        </Notice>
      )}

      {result && (
        <div className="mt-4">
          <Notice
            tone={result.imported ? "ok" : "info"}
            action={
              <Link href="/watchlist" className="font-medium underline underline-offset-4">
                Open watchlist
              </Link>
            }
          >
            {(result.imported ?? 0).toLocaleString("en-US")} added,{" "}
            {(result.duplicates ?? 0).toLocaleString("en-US")} already on your list.
            {problems.length > 0 ? " Some rows were not imported; the notes are below." : ""}
          </Notice>
          {problems.length > 0 && (
            <details className="mt-3 text-sm">
              <summary className="text-muted hover:text-fg">
                Notes from the import ({problems.length.toLocaleString("en-US")})
              </summary>
              <ul className="mt-2 max-h-72 space-y-1 overflow-y-auto font-mono text-xs text-muted">
                {problems.map((problem, index) => (
                  <li key={index}>{describeRow(problem)}</li>
                ))}
              </ul>
            </details>
          )}
        </div>
      )}
    </div>
  );
}

export default function ImportSection({ imdbSync }: { imdbSync: ImdbSync | null }) {
  return (
    <div className="space-y-10">
      <ImdbLink initial={imdbSync} />
      <div className="border-t border-line pt-10">
        <CsvImport />
      </div>
    </div>
  );
}
