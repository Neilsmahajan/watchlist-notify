import type { WatchlistStatus, WatchlistType } from "./types";

// The watchlist's filters live in the address bar, so a filtered view can be
// shared and the back button undoes a filter. This module is the one place
// that reads and writes those parameters, on the server and in the browser.

export const PAGE_SIZE = 48;

export type SortKey =
  | "-added_at"
  | "added_at"
  | "title"
  | "-year"
  | "-rating"
  | "runtime"
  | "available";

export const SORTS: { value: SortKey; label: string }[] = [
  { value: "-added_at", label: "Recently added" },
  { value: "added_at", label: "Oldest first" },
  { value: "available", label: "On my services first" },
  { value: "title", label: "Title A–Z" },
  { value: "-year", label: "Newest release" },
  { value: "-rating", label: "Highest rated" },
  { value: "runtime", label: "Shortest" },
];

export const DEFAULT_SORT: SortKey = "-added_at";

export type Filters = {
  status: WatchlistStatus | "";
  type: WatchlistType | "";
  services: string[];
  available: boolean;
  search: string;
  sort: SortKey;
};

export const NO_FILTERS: Filters = {
  status: "",
  type: "",
  services: [],
  available: false,
  search: "",
  sort: DEFAULT_SORT,
};

const STATUSES = ["planned", "watching", "finished"];
const TYPES = ["movie", "show"];
const MAX_SEARCH = 64;

type ParamSource =
  | URLSearchParams
  | { get(name: string): string | null }
  | Record<string, string | string[] | undefined>;

function read(source: ParamSource, name: string): string {
  if (typeof (source as URLSearchParams).get === "function") {
    return (source as URLSearchParams).get(name) ?? "";
  }
  const value = (source as Record<string, string | string[] | undefined>)[name];
  return (Array.isArray(value) ? value[0] : value) ?? "";
}

// parseFilters accepts anything and returns only valid filters, so a
// hand-edited address never produces a request the API rejects.
export function parseFilters(source: ParamSource): Filters {
  const status = read(source, "status");
  const type = read(source, "type");
  const sort = read(source, "sort");
  const services = read(source, "service")
    .split(",")
    .map((code) => code.trim().toLowerCase())
    .filter((code, index, all) => /^[a-z0-9_]{2,32}$/.test(code) && all.indexOf(code) === index)
    .slice(0, 18);
  return {
    status: STATUSES.includes(status) ? (status as WatchlistStatus) : "",
    type: TYPES.includes(type) ? (type as WatchlistType) : "",
    services,
    available: read(source, "available") === "true",
    search: read(source, "q").trim().slice(0, MAX_SEARCH),
    sort: SORTS.some((s) => s.value === sort) ? (sort as SortKey) : DEFAULT_SORT,
  };
}

// The address-bar form: short, defaults left out, stable order.
export function filtersToSearch(filters: Filters): string {
  const params = new URLSearchParams();
  if (filters.search) params.set("q", filters.search);
  if (filters.status) params.set("status", filters.status);
  if (filters.type) params.set("type", filters.type);
  if (filters.available) params.set("available", "true");
  if (filters.services.length) params.set("service", filters.services.join(","));
  if (filters.sort !== DEFAULT_SORT) params.set("sort", filters.sort);
  return params.toString();
}

// The API form of the same filters, for one page of results.
export function filtersToApiQuery(
  filters: Filters,
  offset = 0,
  limit = PAGE_SIZE,
): string {
  const params = new URLSearchParams();
  if (filters.search) params.set("search", filters.search);
  if (filters.status) params.set("status", filters.status);
  if (filters.type) params.set("type", filters.type);
  if (filters.available) params.set("available", "true");
  if (filters.services.length) params.set("service", filters.services.join(","));
  params.set("sort", filters.sort);
  params.set("limit", String(limit));
  if (offset > 0) params.set("offset", String(offset));
  return params.toString();
}

export function hasActiveFilters(filters: Filters): boolean {
  return Boolean(
    filters.status ||
      filters.type ||
      filters.available ||
      filters.search ||
      filters.services.length,
  );
}
