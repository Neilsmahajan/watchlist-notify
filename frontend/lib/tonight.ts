import { redirectToLogin } from "@/lib/auth/client";
import type { WatchlistItem } from "@/lib/watchlist/types";

// "What should I watch tonight?": the browser's side of
// POST /api/recommendations/tonight. Every answer the API can give becomes
// one case of TonightOutcome, so the panel has a state for each and nothing
// is left to a catch-all.

export type TonightPick = { item: WatchlistItem; reason: string };

// Why there is nothing to pick from.
export type EmptyReason = "no_services" | "nothing_streaming" | "no_match" | "exhausted";

export type TonightRequest = {
  mood?: string;
  maxMinutes?: number;
  // IDs of the titles already shown, so the next three are different.
  exclude?: string[];
};

export type TonightOutcome =
  // remaining is how many more picks the user can ask for today.
  | { kind: "picks"; picks: TonightPick[]; remaining: number | null }
  | { kind: "empty"; reason: EmptyReason; message: string; remaining: number | null }
  // A daily limit is used up: the user's own, or the one everyone shares.
  | { kind: "limit"; scope: "user" | "total"; resetsAt: string | null }
  // Trying again may help unless retry is false.
  | { kind: "failed"; message: string; retry: boolean };

// The API accepts at most this many excluded IDs.
export const MAX_EXCLUDE = 30;

const EMPTY_REASONS: EmptyReason[] = ["no_services", "nothing_streaming", "no_match", "exhausted"];

const EMPTY_FALLBACK = "Nothing on your list fits tonight.";
const FAILED = "The picks could not be made just now.";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function readPicks(value: unknown): TonightPick[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((pick) => {
    if (!isRecord(pick) || !isRecord(pick.item)) return [];
    const { item } = pick;
    if (typeof item.id !== "string" || typeof item.title !== "string") return [];
    return [
      {
        item: item as unknown as WatchlistItem,
        reason: typeof pick.reason === "string" ? pick.reason : "",
      },
    ];
  });
}

function readTime(value: unknown): string | null {
  return typeof value === "string" && !Number.isNaN(Date.parse(value)) ? value : null;
}

export async function pickTonight(
  request: TonightRequest,
  signal?: AbortSignal,
): Promise<TonightOutcome> {
  const payload: Record<string, unknown> = {};
  if (request.mood) payload.mood = request.mood;
  if (request.maxMinutes) payload.max_minutes = request.maxMinutes;
  if (request.exclude?.length) payload.exclude = request.exclude.slice(-MAX_EXCLUDE);

  let response: Response;
  try {
    response = await fetch("/api/recommendations/tonight", {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify(payload),
      signal,
    });
  } catch (error) {
    // A request the panel cancelled is the panel's to ignore.
    if (signal?.aborted) throw error;
    return { kind: "failed", message: "The service could not be reached.", retry: true };
  }

  const parsed: unknown = await response.json().catch(() => null);
  const body = isRecord(parsed) ? parsed : {};

  if (response.status === 401) {
    redirectToLogin();
    return { kind: "failed", message: "Your session has expired. Signing you in again.", retry: false };
  }
  if (response.status === 429) {
    return {
      kind: "limit",
      scope: body.scope === "total" ? "total" : "user",
      resetsAt: readTime(body.resets_at),
    };
  }
  if (response.status === 503) {
    return { kind: "failed", message: "Picks are switched off at the moment.", retry: false };
  }
  if (response.status === 504) {
    return { kind: "failed", message: "That took too long.", retry: true };
  }
  if (!response.ok) {
    return { kind: "failed", message: FAILED, retry: response.status !== 400 && response.status !== 403 };
  }

  const remaining = typeof body.remaining_today === "number" ? body.remaining_today : null;
  const picks = readPicks(body.picks);
  if (picks.length > 0) return { kind: "picks", picks, remaining };

  const reason = EMPTY_REASONS.find((known) => known === body.empty_reason) ?? "nothing_streaming";
  const message = typeof body.message === "string" && body.message ? body.message : EMPTY_FALLBACK;
  return { kind: "empty", reason, message, remaining };
}
