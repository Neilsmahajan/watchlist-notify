// TMDB serves images straight from its CDN in fixed widths. Posters and logos
// are requested from there directly, never through Vercel's image optimizer:
// the CDN already resizes, and the optimizer would bill for every poster.

const IMAGE_BASE = "https://image.tmdb.org/t/p";

export const POSTER_WIDTHS = [185, 342, 500] as const;

export function posterUrl(path: string, width: number = 342): string {
  return `${IMAGE_BASE}/w${width}${path}`;
}

export function posterSrcSet(path: string): string {
  return POSTER_WIDTHS.map((w) => `${posterUrl(path, w)} ${w}w`).join(", ");
}

export function logoUrl(path: string): string {
  return `${IMAGE_BASE}/w92${path}`;
}

// The search endpoint returns a full poster URL. Recover the path so results
// can use the same srcset as everything else.
export function posterPathFromUrl(url?: string | null): string | undefined {
  if (!url) return undefined;
  const match = url.match(/\/t\/p\/[a-z0-9]+(\/[^/?#]+)$/i);
  return match ? match[1] : undefined;
}
