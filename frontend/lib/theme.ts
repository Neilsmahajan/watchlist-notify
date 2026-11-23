// The theme is a data-theme attribute on <html>, set before first paint by
// the inline script in the root layout. No attribute means "follow the
// system". CSS does the rest (see globals.css), so no component needs to
// know the theme to render, and nothing about it can differ between the
// server's HTML and the browser's first render.

export const THEME_STORAGE_KEY = "watchlist-notify-theme";
export const THEME_EVENT = "watchlist-notify-theme-change";

export type ThemeChoice = "system" | "light" | "dark";

// Runs in <head>, before anything is painted.
export const themeScript = `(function(){try{var t=localStorage.getItem(${JSON.stringify(
  THEME_STORAGE_KEY,
)});if(t==="light"||t==="dark"){document.documentElement.dataset.theme=t}}catch(e){}})()`;

export function readThemeChoice(): ThemeChoice {
  const value = document.documentElement.dataset.theme;
  return value === "light" || value === "dark" ? value : "system";
}

export function systemPrefersDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export function applyThemeChoice(choice: ThemeChoice): void {
  const root = document.documentElement;
  if (choice === "system") {
    delete root.dataset.theme;
  } else {
    root.dataset.theme = choice;
  }
  try {
    if (choice === "system") {
      window.localStorage.removeItem(THEME_STORAGE_KEY);
    } else {
      window.localStorage.setItem(THEME_STORAGE_KEY, choice);
    }
  } catch {
    // Private browsing: the choice lasts for this page only.
  }
  window.dispatchEvent(new Event(THEME_EVENT));
}

// The header button flips whatever is showing now. Landing back on the
// system's own theme clears the override, so the site follows the system
// again instead of pinning its current value.
export function toggleTheme(): void {
  const current = readThemeChoice();
  const showingDark =
    current === "dark" || (current === "system" && systemPrefersDark());
  const next = showingDark ? "light" : "dark";
  applyThemeChoice(
    (next === "dark") === systemPrefersDark() ? "system" : next,
  );
}
