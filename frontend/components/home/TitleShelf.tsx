"use client";

import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import TitleDialog, { type DialogActions } from "@/components/watchlist/TitleDialog";
import TitleTile from "@/components/watchlist/TitleTile";
import type { ServiceNames } from "@/lib/services";
import { recheckItem, removeItem, setItemStatus } from "@/lib/watchlist/client";
import type { WatchlistItem } from "@/lib/watchlist/types";

// One row of posters that scrolls sideways, on the dashboard. The tiles and
// the detail view are the watchlist's own; a change made here asks the server
// for the page again so the counts around the row follow.
export default function TitleShelf({
  items: initial,
  names,
  label,
}: {
  items: WatchlistItem[];
  names: ServiceNames;
  label: string;
}) {
  const router = useRouter();
  const [items, setItems] = useState(initial);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  // The server sends a new list after each refresh.
  useEffect(() => setItems(initial), [initial]);

  const actions = useMemo<DialogActions>(
    () => ({
      async onStatus(item, status) {
        await setItemStatus(item, status);
        setItems((all) => all.map((i) => (i.id === item.id ? { ...i, status } : i)));
        router.refresh();
      },
      async onRemove(item) {
        await removeItem(item);
        setSelectedId(null);
        setItems((all) => all.filter((i) => i.id !== item.id));
        router.refresh();
      },
      async onRecheck(item) {
        const fresh = await recheckItem(item);
        if (fresh) setItems((all) => all.map((i) => (i.id === fresh.id ? fresh : i)));
        router.refresh();
      },
    }),
    [router],
  );

  const selected = selectedId ? (items.find((item) => item.id === selectedId) ?? null) : null;

  return (
    <>
      <ul
        aria-label={label}
        className="scroll-row -mx-4 scroll-px-4 gap-3 px-4 pb-2 sm:-mx-6 sm:scroll-px-6 sm:gap-4 sm:px-6 lg:-mx-8 lg:scroll-px-8 lg:px-8 [&>li]:w-[29%] [&>li]:shrink-0 [&>li]:snap-start sm:[&>li]:w-[150px] lg:[&>li]:w-[168px]"
      >
        {items.map((item, index) => (
          <TitleTile
            key={item.id}
            item={item}
            names={names}
            onOpen={(opened) => setSelectedId(opened.id)}
            eager={index < 4}
          />
        ))}
      </ul>
      <TitleDialog
        item={selected}
        names={names}
        onClose={() => setSelectedId(null)}
        actions={actions}
      />
    </>
  );
}
