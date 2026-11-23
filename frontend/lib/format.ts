// Everything here renders the same string on the server and in the browser.
// Formatting in the visitor's locale or time zone (toLocaleDateString() with
// no arguments) is what caused the hydration error: the server is in UTC and
// en-US, the browser is wherever the visitor is.

const DATE = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  year: "numeric",
  timeZone: "UTC",
});

const DATE_TIME = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  year: "numeric",
  hour: "numeric",
  minute: "2-digit",
  timeZone: "UTC",
  timeZoneName: "short",
});

function parse(value?: string | null): Date | null {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) || date.getUTCFullYear() < 1971
    ? null
    : date;
}

export function formatDate(value?: string | null): string | null {
  const date = parse(value);
  return date ? DATE.format(date) : null;
}

export function formatDateTime(value?: string | null): string | null {
  const date = parse(value);
  return date ? DATE_TIME.format(date) : null;
}

export function formatRuntime(minutes?: number | null): string | null {
  if (!minutes || minutes <= 0) return null;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  if (!hours) return `${rest}m`;
  return rest ? `${hours}h ${rest}m` : `${hours}h`;
}

export function formatRating(rating?: number | null): string | null {
  if (!rating || rating <= 0) return null;
  return rating.toFixed(1);
}

export function formatCount(count: number, one: string, many?: string): string {
  return `${count.toLocaleString("en-US")} ${count === 1 ? one : (many ?? `${one}s`)}`;
}
