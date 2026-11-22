export type WatchlistType = "movie" | "show";
export type WatchlistStatus = "planned" | "watching" | "finished";

export type WatchlistItem = {
  id: string;
  title: string;
  type: WatchlistType;
  status: WatchlistStatus;
  year?: number;
  tmdb_id?: number;
  imdb_id?: string;
  tags?: string[];
  added_at?: string;
  updated_at?: string;
  // Title details and availability, attached by GET /api/watchlist.
  poster_path?: string;
  runtime?: number;
  genres?: string[];
  rating?: number;
  overview?: string;
  availability?: WatchlistAvailability | null;
};

// One provider in an item's embedded availability. Providers that are not
// one of the supported services have no code.
export type WatchlistAvailabilityProvider = {
  code?: string;
  name: string;
  logo_path?: string;
  access?: string[];
};

// Where a title streams in the user's region, as stored by the backend.
export type WatchlistAvailability = {
  region: string;
  checked_at?: string;
  link?: string;
  // The user's active services streaming the title.
  providers?: WatchlistAvailabilityProvider[];
  // Every other offer: services the user does not have, rent and buy.
  elsewhere?: WatchlistAvailabilityProvider[];
};

export type WatchlistFacets = {
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
  facets?: WatchlistFacets;
};

export type WatchlistResponse = {
  items?: WatchlistItem[];
  meta?: WatchlistMeta;
  error?: string;
};

export type WatchlistImportRowError = {
  row: number;
  reason: string;
};

export type WatchlistImportResponse = {
  imported?: number;
  duplicates?: number;
  errors?: WatchlistImportRowError[];
  error?: string;
};

export type AvailabilityAccess = "subscription" | "free" | "ads";

export const AVAILABILITY_ACCESS_ORDER: AvailabilityAccess[] = [
  "subscription",
  "free",
  "ads",
];

export const AVAILABILITY_ACCESS_META: Record<
  AvailabilityAccess,
  { label: string; description: string; chipClass: string; badgeClass: string }
> = {
  subscription: {
    label: "Subscription",
    description: "Included with your paid plan",
    chipClass:
      "bg-emerald-50 text-emerald-700 border border-emerald-100 hover:border-emerald-200",
    badgeClass: "bg-emerald-100 text-emerald-800",
  },
  free: {
    label: "Free",
    description: "Free to stream",
    chipClass:
      "bg-sky-50 text-sky-700 border border-sky-100 hover:border-sky-200",
    badgeClass: "bg-sky-100 text-sky-800",
  },
  ads: {
    label: "Free with ads",
    description: "Ad-supported access",
    chipClass:
      "bg-amber-50 text-amber-700 border border-amber-100 hover:border-amber-200",
    badgeClass: "bg-amber-100 text-amber-800",
  },
};

export type AvailabilityProvider = {
  code: string;
  name: string;
  logo_path?: string;
  link?: string;
  access?: AvailabilityAccess[];
};

export type AvailabilityResponse = {
  region: string;
  providers: AvailabilityProvider[];
  unmatched_user_services?: string[];
  error?: string;
};

export type ServiceAccess = AvailabilityAccess;

export type UserService = {
  code: string;
  name: string;
  active: boolean;
  added_at?: string;
  access?: ServiceAccess;
};

export type ServiceResponse = {
  services?: UserService[];
  error?: string;
};

export type StatSummary = {
  label: string;
  value: string;
  color: string;
  loading: boolean;
};

export const typeLabels: Record<WatchlistType, string> = {
  movie: "Movie",
  show: "TV",
};

export const TMDB_IMAGE_BASE = "https://media.themoviedb.org/t/p/original";

export const DEFAULT_AVAILABILITY_ACCESS: AvailabilityAccess = "subscription";

export const getPrimaryAccess = (
  provider: AvailabilityProvider,
): AvailabilityAccess => {
  const [first] = provider.access ?? [];
  return first ?? DEFAULT_AVAILABILITY_ACCESS;
};

const isStreamingAccess = (value: string): value is AvailabilityAccess =>
  (AVAILABILITY_ACCESS_ORDER as string[]).includes(value);

// availabilityFromItem reads the availability the backend embedded in a
// watchlist item, in the shape the availability endpoint returns. It is null
// for an item whose availability is not known (yet).
export const availabilityFromItem = (
  item: WatchlistItem,
): AvailabilityResponse | null => {
  const embedded = item.availability;
  if (!embedded) {
    return null;
  }
  const providers: AvailabilityProvider[] = [];
  for (const provider of embedded.providers ?? []) {
    if (!provider?.code) {
      continue;
    }
    providers.push({
      code: provider.code,
      name: provider.name,
      logo_path: provider.logo_path,
      link: embedded.link,
      access: (provider.access ?? []).filter(isStreamingAccess),
    });
  }
  return { region: embedded.region, providers };
};

// availabilityByItemId maps every item to its embedded availability.
export const availabilityByItemId = (
  items: WatchlistItem[],
): Record<string, AvailabilityResponse | null> => {
  const byId: Record<string, AvailabilityResponse | null> = {};
  for (const item of items) {
    byId[item.id] = availabilityFromItem(item);
  }
  return byId;
};
