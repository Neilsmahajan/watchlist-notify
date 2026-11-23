"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import Logo from "@/components/Logo";
import ThemeToggle from "@/components/ThemeToggle";
import { Button, cx } from "@/components/ui";
import {
  IconHome,
  IconList,
  IconSearch,
  IconSettings,
} from "@/components/icons";

export type HeaderUser = {
  name: string;
  email: string;
  picture: string;
};

const LINKS = [
  { href: "/", label: "Home", icon: IconHome },
  { href: "/watchlist", label: "Watchlist", icon: IconList },
  { href: "/search", label: "Search", icon: IconSearch },
  { href: "/settings", label: "Settings", icon: IconSettings },
];

function isCurrent(pathname: string, href: string): boolean {
  return href === "/" ? pathname === "/" : pathname.startsWith(href);
}

// The signed-in state arrives from the server with the page, so the header
// is right on first paint instead of filling in after a request.
export default function SiteHeader({ user }: { user: HeaderUser | null }) {
  const pathname = usePathname() || "/";

  return (
    <>
      <header className="sticky top-0 z-40 border-b border-line bg-bg/85 backdrop-blur-md">
        <div className="mx-auto flex h-14 max-w-[1400px] items-center gap-3 px-4 sm:h-16 sm:px-6 lg:px-8">
          <Link href="/" aria-label="Watchlist Notify, home" className="shrink-0">
            <Logo />
          </Link>

          {user && (
            <nav aria-label="Main" className="ml-6 hidden items-center gap-1 md:flex">
              {LINKS.map((link) => {
                const current = isCurrent(pathname, link.href);
                return (
                  <Link
                    key={link.href}
                    href={link.href}
                    aria-current={current ? "page" : undefined}
                    className={cx(
                      "rounded-full px-3.5 py-1.5 text-sm transition-colors",
                      current
                        ? "bg-surface-2 font-medium text-fg"
                        : "text-muted hover:text-fg",
                    )}
                  >
                    {link.label}
                  </Link>
                );
              })}
            </nav>
          )}

          <div className="ml-auto flex items-center gap-1 sm:gap-2">
            {!user && (
              <Link
                href="/demo"
                aria-current={pathname === "/demo" ? "page" : undefined}
                className="rounded-full px-3 py-1.5 text-sm text-muted transition-colors hover:text-fg aria-[current=page]:text-fg"
              >
                Demo
              </Link>
            )}
            <ThemeToggle />
            {user ? (
              <AccountMenu user={user} />
            ) : (
              <Button href="/auth/login" size="sm" variant="ink">
                Sign in
              </Button>
            )}
          </div>
        </div>
      </header>

      {user && (
        <nav
          aria-label="Main"
          className="fixed inset-x-0 bottom-0 z-40 border-t border-line bg-bg/95 pb-[env(safe-area-inset-bottom)] backdrop-blur-md md:hidden"
        >
          <ul className="mx-auto grid max-w-md grid-cols-4">
            {LINKS.map((link) => {
              const current = isCurrent(pathname, link.href);
              const LinkIcon = link.icon;
              return (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    aria-current={current ? "page" : undefined}
                    className={cx(
                      "flex h-14 flex-col items-center justify-center gap-0.5 text-[11px] transition-colors",
                      current ? "text-fg" : "text-faint",
                    )}
                  >
                    <LinkIcon
                      className={cx("size-5", current && "text-accent-text")}
                    />
                    {link.label}
                  </Link>
                </li>
              );
            })}
          </ul>
        </nav>
      )}
    </>
  );
}

function Avatar({ user }: { user: HeaderUser }) {
  const [failed, setFailed] = useState(false);
  const initial = (user.name || user.email || "?").trim().charAt(0).toUpperCase();
  if (user.picture && !failed) {
    return (
      <img
        src={user.picture}
        alt=""
        width={32}
        height={32}
        referrerPolicy="no-referrer"
        onError={() => setFailed(true)}
        className="size-8 rounded-full border border-line object-cover"
      />
    );
  }
  return (
    <span className="flex size-8 items-center justify-center rounded-full bg-fg text-[13px] font-semibold text-bg">
      {initial}
    </span>
  );
}

function AccountMenu({ user }: { user: HeaderUser }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const pathname = usePathname();

  useEffect(() => setOpen(false), [pathname]);

  useEffect(() => {
    if (!open) return;
    const onPointer = (event: PointerEvent) => {
      if (!ref.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="Account menu"
        className="flex items-center rounded-full p-0.5 transition-opacity hover:opacity-80"
      >
        <Avatar user={user} />
      </button>
      {open && (
        <div
          role="menu"
          className="absolute right-0 mt-2 w-60 origin-top-right animate-fade overflow-hidden rounded-xl border border-line bg-surface shadow-pop"
        >
          <div className="border-b border-line px-4 py-3">
            <p className="truncate text-sm font-medium">{user.name || "Signed in"}</p>
            <p className="truncate text-xs text-muted">{user.email}</p>
          </div>
          <div className="p-1.5 text-sm">
            <Link role="menuitem" href="/profile" className="block rounded-lg px-3 py-2 hover:bg-surface-2">
              Profile
            </Link>
            <Link role="menuitem" href="/settings" className="block rounded-lg px-3 py-2 hover:bg-surface-2">
              Settings
            </Link>
            <a role="menuitem" href="/auth/logout" className="block rounded-lg px-3 py-2 text-muted hover:bg-surface-2 hover:text-fg">
              Sign out
            </a>
          </div>
        </div>
      )}
    </div>
  );
}
