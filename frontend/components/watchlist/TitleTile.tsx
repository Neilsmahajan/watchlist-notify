import Poster from "@/components/Poster";
import ServiceLogo from "@/components/ServiceLogo";
import { cx } from "@/components/ui";
import { formatRuntime } from "@/lib/format";
import { serviceName, type ServiceNames } from "@/lib/services";
import type { WatchlistItem } from "@/lib/watchlist/types";

export const GRID_SIZES =
  "(min-width: 1360px) 180px, (min-width: 1100px) 15vw, (min-width: 900px) 18vw, (min-width: 640px) 22vw, 30vw";

const MAX_LOGOS = 3;

type TitleTileProps = {
  item: WatchlistItem;
  names: ServiceNames;
  onOpen: (item: WatchlistItem) => void;
  eager?: boolean;
};

// One title in the grid: the poster, where it streams on the user's
// services, and its name. The whole tile is one button that opens the detail.
export default function TitleTile({ item, names, onOpen, eager }: TitleTileProps) {
  const providers = item.availability?.providers ?? [];
  const available = providers.length > 0;
  const providerNames = providers.map((p) => serviceName(p.code, names, p.name));
  const slate = [item.year, formatRuntime(item.runtime)].filter(Boolean).join(" · ");

  return (
    <li className="min-w-0">
      <button
        type="button"
        onClick={() => onOpen(item)}
        aria-haspopup="dialog"
        className="group relative block w-full rounded-lg text-left"
      >
        <span className="relative block">
          <Poster
            path={item.poster_path}
            title={item.title}
            year={item.year}
            sizes={GRID_SIZES}
            eager={eager}
            decorative
            className={cx(
              "ring-1 ring-line transition-[transform,box-shadow,filter] duration-200 group-hover:-translate-y-0.5 group-hover:shadow-lift group-hover:ring-line-strong group-focus-visible:ring-accent",
              !available && "saturate-[0.55] brightness-[0.82] group-hover:saturate-100 group-hover:brightness-100",
            )}
          />

          {item.status !== "planned" && (
            <span
              className={cx(
                "slate absolute left-1.5 top-1.5 rounded-full px-2 py-[3px] text-[10px] font-medium shadow-sm",
                item.status === "watching"
                  ? "bg-accent text-accent-ink"
                  : "bg-black/75 text-white/90 backdrop-blur-sm",
              )}
            >
              {item.status === "watching" ? "Watching" : "Finished"}
            </span>
          )}

          {available && (
            <span className="pointer-events-none absolute inset-x-0 bottom-0 flex items-end gap-1 rounded-b-lg bg-gradient-to-t from-black/85 via-black/45 to-transparent px-1.5 pb-1.5 pt-7">
              {providers.slice(0, MAX_LOGOS).map((provider, index) => (
                <ServiceLogo
                  key={provider.code ?? provider.name}
                  code={provider.code}
                  name={providerNames[index]}
                  logoPath={provider.logo_path}
                  labelled
                  className="size-[22px] ring-1 ring-white/25 sm:size-6"
                />
              ))}
              {providers.length > MAX_LOGOS && (
                <span className="slate pb-1 pl-0.5 text-[10px] text-white/90">
                  +{providers.length - MAX_LOGOS}
                </span>
              )}
            </span>
          )}
        </span>

        <span className="mt-2.5 block text-[13px] font-medium leading-snug [overflow-wrap:anywhere] line-clamp-2 sm:text-sm">
          {item.title}
        </span>
        {slate && <span className="slate mt-1 block text-faint">{slate}</span>}
        {/* Positioned against the tile (the button is relative), so it stays
            inside a row that scrolls sideways. */}
        <span className="sr-only">
          {available
            ? `. Streaming on ${providerNames.join(", ")}.`
            : ". Not on your services."}
          {item.status !== "planned" ? ` Status: ${item.status}.` : ""}
        </span>
      </button>
    </li>
  );
}
