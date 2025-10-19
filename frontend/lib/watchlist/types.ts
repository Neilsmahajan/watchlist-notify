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
};

export type WatchlistResponse = {
  items?: WatchlistItem[];
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
