"use client";

import { useEffect, useRef, useState } from "react";
import { posterSrcSet, posterUrl } from "@/lib/tmdb";
import { cx } from "@/components/ui";

type PosterProps = {
  path?: string;
  title: string;
  year?: number;
  // How wide the poster is drawn, for the browser to pick a file: a CSS
  // sizes value.
  sizes: string;
  eager?: boolean;
  className?: string;
  // Decorative posters (a wall of them, or one next to its own title) are
  // hidden from screen readers.
  decorative?: boolean;
};

// A poster in a fixed 2:3 frame, so the page never moves as images arrive.
// Without a poster, or if the image fails, the frame shows a designed tile
// with the title instead of a broken image.
export default function Poster({
  path,
  title,
  year,
  sizes,
  eager = false,
  className,
  decorative = false,
}: PosterProps) {
  const [failed, setFailed] = useState(false);
  const ref = useRef<HTMLImageElement>(null);

  // An image can fail before React attaches onError. A new path starts over.
  useEffect(() => {
    const img = ref.current;
    setFailed(Boolean(img && img.complete && img.naturalWidth === 0));
  }, [path]);

  return (
    <div
      className={cx(
        "relative aspect-[2/3] w-full overflow-hidden rounded-lg bg-poster-empty [container-type:inline-size]",
        className,
      )}
    >
      {path && !failed ? (
        <img
          ref={ref}
          src={posterUrl(path, 342)}
          srcSet={posterSrcSet(path)}
          sizes={sizes}
          alt={decorative ? "" : `Poster for ${title}`}
          loading={eager ? "eager" : "lazy"}
          decoding="async"
          onError={() => setFailed(true)}
          className="absolute inset-0 size-full object-cover"
        />
      ) : (
        <div
          className="absolute inset-0 flex flex-col justify-between border border-line p-[9%]"
          aria-hidden={decorative || undefined}
        >
          <span className="slate text-faint">{year || "No poster"}</span>
          <span className="font-display text-[clamp(0.7rem,11cqw,1.5rem)] font-medium leading-[1.05] tracking-tight text-muted [overflow-wrap:anywhere] line-clamp-5">
            {title}
          </span>
          <span className="h-px w-1/3 bg-accent" />
        </div>
      )}
    </div>
  );
}
