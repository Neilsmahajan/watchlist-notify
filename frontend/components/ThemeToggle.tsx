"use client";

import { toggleTheme } from "@/lib/theme";
import { IconMoon, IconSun } from "@/components/icons";

// Both icons are always rendered and CSS shows the right one, so this button
// is the same on the server and in the browser whatever the theme is.
export default function ThemeToggle({ className = "" }: { className?: string }) {
  return (
    <button
      type="button"
      onClick={toggleTheme}
      aria-label="Switch between light and dark theme"
      title="Switch theme"
      className={`inline-flex size-9 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-2 hover:text-fg ${className}`}
    >
      <IconMoon className="size-[18px] dark:hidden" />
      <IconSun className="hidden size-[18px] dark:block" />
    </button>
  );
}
