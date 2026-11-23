"use client";

import { useEffect, useRef, useState } from "react";
import ServiceLogo from "@/components/ServiceLogo";
import { cx } from "@/components/ui";
import { IconChevronDown, IconClose, IconSearch } from "@/components/icons";
import { serviceName, type ServiceNames } from "@/lib/services";
import { SORTS, type Filters, type SortKey } from "@/lib/watchlist/query";
import {
  STATUS_LABELS,
  type Facets,
  type WatchlistStatus,
  type WatchlistType,
} from "@/lib/watchlist/types";

type FilterBarProps = {
  filters: Filters;
  facets?: Facets;
  names: ServiceNames;
  // Codes of the services the list's owner has.
  mine: Set<string>;
  // replace: true rewrites the current history entry (typing in the search
  // box) instead of adding one.
  onChange: (next: Filters, options?: { replace?: boolean }) => void;
};

const STATUSES: WatchlistStatus[] = ["planned", "watching", "finished"];
const TYPES: { value: WatchlistType | ""; label: string }[] = [
  { value: "", label: "All" },
  { value: "movie", label: "Films" },
  { value: "show", label: "Series" },
];

const SEGMENT =
  "inline-flex shrink-0 snap-start rounded-full border border-line bg-surface p-0.5";
const SEGMENT_ITEM =
  "rounded-full px-3 py-1.5 text-[13px] whitespace-nowrap transition-colors";

export default function FilterBar({ filters, facets, names, mine, onChange }: FilterBarProps) {
  const [text, setText] = useState(filters.search);
  const typed = useRef(filters.search);

  // The address bar is the source of truth: when it changes under us (back
  // button, "clear filters"), the box follows.
  useEffect(() => {
    if (filters.search !== typed.current) {
      typed.current = filters.search;
      setText(filters.search);
    }
  }, [filters.search]);

  // Typing updates the address, and with it the list, after a short pause.
  useEffect(() => {
    const value = text.trim();
    if (value === typed.current) return;
    const timer = window.setTimeout(() => {
      typed.current = value;
      onChange({ ...filters, search: value }, { replace: true });
    }, 280);
    return () => window.clearTimeout(timer);
    // Only the text restarts the timer.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [text]);

  const statusCounts = facets?.status ?? {};
  const allCount = STATUSES.reduce((sum, s) => sum + (statusCounts[s] ?? 0), 0);

  const serviceCounts = facets?.services ?? {};
  const chips = Array.from(new Set([...Object.keys(serviceCounts), ...filters.services]))
    .map((code) => ({ code, count: serviceCounts[code] ?? 0, mine: mine.has(code) }))
    .sort(
      (a, b) =>
        Number(b.mine) - Number(a.mine) ||
        b.count - a.count ||
        a.code.localeCompare(b.code),
    );

  const toggleService = (code: string) => {
    const services = filters.services.includes(code)
      ? filters.services.filter((c) => c !== code)
      : [...filters.services, code];
    onChange({ ...filters, services });
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center">
        <label className="relative block lg:w-72 lg:shrink-0">
          <span className="sr-only">Search this list</span>
          <IconSearch className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-faint" />
          <input
            type="search"
            value={text}
            onChange={(event) => setText(event.target.value)}
            maxLength={64}
            placeholder="Search this list"
            autoComplete="off"
            className="h-10 w-full rounded-full border border-line bg-surface pl-10 pr-9 text-sm placeholder:text-faint focus:border-accent focus:outline-none"
          />
          {text && (
            <button
              type="button"
              onClick={() => setText("")}
              aria-label="Clear search"
              className="absolute right-2 top-1/2 inline-flex size-6 -translate-y-1/2 items-center justify-center rounded-full text-faint hover:text-fg"
            >
              <IconClose className="size-3.5" />
            </button>
          )}
        </label>

        <div className="scroll-row -mx-4 gap-2 px-4 sm:mx-0 sm:flex-wrap sm:px-0 lg:flex-1">
          <button
            type="button"
            aria-pressed={filters.available}
            onClick={() => onChange({ ...filters, available: !filters.available })}
            className={cx(
              "inline-flex h-[38px] shrink-0 snap-start items-center gap-2 rounded-full border px-3.5 text-[13px] font-medium transition-colors",
              filters.available
                ? "border-accent bg-accent text-accent-ink"
                : "border-line bg-surface text-fg hover:border-line-strong",
            )}
          >
            <span
              className={cx(
                "size-2 rounded-full",
                filters.available ? "bg-accent-ink" : "bg-accent",
              )}
              aria-hidden="true"
            />
            On my services
            {facets?.available !== undefined && (
              <span className="slate opacity-70">{facets.available}</span>
            )}
          </button>

          <div role="group" aria-label="Status" className={SEGMENT}>
            <SegmentButton
              selected={!filters.status}
              onClick={() => onChange({ ...filters, status: "" })}
              count={facets ? allCount : undefined}
            >
              All
            </SegmentButton>
            {STATUSES.map((status) => (
              <SegmentButton
                key={status}
                selected={filters.status === status}
                onClick={() => onChange({ ...filters, status })}
                count={facets ? (statusCounts[status] ?? 0) : undefined}
              >
                {STATUS_LABELS[status]}
              </SegmentButton>
            ))}
          </div>

          <div role="group" aria-label="Type" className={SEGMENT}>
            {TYPES.map((type) => (
              <SegmentButton
                key={type.value || "all"}
                selected={filters.type === type.value}
                onClick={() => onChange({ ...filters, type: type.value })}
              >
                {type.label}
              </SegmentButton>
            ))}
          </div>

          <label className="relative inline-flex shrink-0 snap-start items-center lg:ml-auto">
            <span className="sr-only">Sort by</span>
            <select
              value={filters.sort}
              onChange={(event) =>
                onChange({ ...filters, sort: event.target.value as SortKey })
              }
              className="h-[38px] appearance-none rounded-full border border-line bg-surface pl-3.5 pr-9 text-[13px] text-fg hover:border-line-strong focus:border-accent focus:outline-none"
            >
              {SORTS.map((sort) => (
                <option key={sort.value} value={sort.value}>
                  {sort.label}
                </option>
              ))}
            </select>
            <IconChevronDown className="pointer-events-none absolute right-3 size-4 text-faint" />
          </label>
        </div>
      </div>

      {chips.length > 0 && (
        <div
          role="group"
          aria-label="Filter by service"
          className="scroll-row -mx-4 gap-2 px-4 sm:mx-0 sm:flex-wrap sm:px-0"
        >
          {chips.map((chip) => {
            const selected = filters.services.includes(chip.code);
            const name = serviceName(chip.code, names);
            return (
              <button
                key={chip.code}
                type="button"
                aria-pressed={selected}
                onClick={() => toggleService(chip.code)}
                title={chip.mine ? `${name} (one of your services)` : name}
                className={cx(
                  "inline-flex h-9 shrink-0 snap-start items-center gap-2 rounded-full border py-1 pl-1 pr-3 text-[13px] transition-colors",
                  selected
                    ? "border-fg bg-fg text-bg"
                    : "border-line bg-surface text-fg hover:border-line-strong",
                  !chip.mine && !selected && "text-muted",
                )}
              >
                <ServiceLogo code={chip.code} name={name} labelled className="size-7 rounded-full" />
                <span className="whitespace-nowrap">{name}</span>
                <span className="slate opacity-60">{chip.count}</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

function SegmentButton({
  selected,
  onClick,
  count,
  children,
}: {
  selected: boolean;
  onClick: () => void;
  count?: number;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onClick}
      className={cx(
        SEGMENT_ITEM,
        selected ? "bg-fg font-medium text-bg" : "text-muted hover:text-fg",
      )}
    >
      {children}
      {count !== undefined && <span className="slate ml-1.5 opacity-60">{count}</span>}
    </button>
  );
}
