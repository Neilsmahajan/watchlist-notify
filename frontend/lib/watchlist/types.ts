export type WatchlistType = "movie" | "show";
export type WatchlistStatus = "planned" | "watching" | "finished";

// One offer in an item's availability. Stores that only rent or sell have no
// service code.
export type Provider = {
  code?: string;
  name: string;
  logo_path?: string;
  access?: string[]; // subscription | free | ads | rent | buy
};

// Where a title can be watched in the user's region, as stored by the API.
export type Availability = {
  region: string;
  checked_at?: string;
  link?: string; // the JustWatch page, via TMDB
  providers?: Provider[]; // the user's services streaming it
  elsewhere?: Provider[]; // other services, rent and buy
};

export type WatchlistItem = {
  id: string;
  title: string;
  type: WatchlistType;
  status: WatchlistStatus;
  year?: number;
  tmdb_id?: number;
  imdb_id?: string;
  tags?: string[];
  source?: "imdb" | "csv";
  // Set when the title was not on the linked IMDb list at the last sync. It
  // is removed at a later sync unless its status is changed here.
  imdb_missing_since?: string;
  added_at?: string;
  updated_at?: string;
  poster_path?: string;
  runtime?: number;
  genres?: string[];
  rating?: number;
  overview?: string;
  availability?: Availability | null;
};

export type Facets = {
  services?: Record<string, number>;
  available?: number;
  unavailable?: number;
  status?: Record<string, number>;
};

export type WatchlistMeta = {
  limit: number;
  offset: number;
  returned: number;
  total: number;
  next_offset?: number;
  region?: string;
  facets?: Facets;
};

export type WatchlistPage = {
  items: WatchlistItem[];
  meta: WatchlistMeta;
};

// An entry of the service catalogue, with whether the user has it.
export type Service = {
  code: string;
  name: string;
  access?: string; // subscription | free | ads
  active: boolean;
  added_at?: string;
};

export type ImdbSync = {
  source_id: string;
  linked_at?: string;
  last_attempt_at?: string;
  last_synced_at?: string;
  last_status?: "ok" | "partial" | "failed" | "not_found";
  last_error?: string;
  total?: number;
  added?: number;
  removed?: number;
  unmatched?: number;
};

export type Me = {
  id?: string;
  email?: string;
  name?: string;
  picture?: string;
  region?: string;
  created_at?: string;
  preferences?: {
    notify_email?: string;
    use_account_email?: boolean;
    marketing_consent?: boolean;
    digest_consent?: boolean;
    digest?: {
      enabled?: boolean;
      interval?: number;
      interval_unit?: string;
      last_sent_at?: string;
      next_scheduled_at?: string;
    };
  };
  imdb_sync?: ImdbSync | null;
  // Optional features this API has switched on. Absent on an older API.
  features?: {
    // "What should I watch tonight?" needs the server to have an Anthropic key.
    recommendations?: boolean;
  };
};

export const STATUS_LABELS: Record<WatchlistStatus, string> = {
  planned: "Planned",
  watching: "Watching",
  finished: "Finished",
};

export const TYPE_LABELS: Record<WatchlistType, string> = {
  movie: "Film",
  show: "Series",
};

export function totalOf(meta?: WatchlistMeta | null): number {
  const facets = meta?.facets;
  if (!facets) return meta?.total ?? 0;
  return (facets.available ?? 0) + (facets.unavailable ?? 0);
}

export function isAvailable(item: WatchlistItem): boolean {
  return Boolean(item.availability?.providers?.length);
}
