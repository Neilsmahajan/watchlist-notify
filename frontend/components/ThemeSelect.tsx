"use client";

import { useSyncExternalStore } from "react";
import {
  THEME_EVENT,
  applyThemeChoice,
  readThemeChoice,
  type ThemeChoice,
} from "@/lib/theme";

const OPTIONS: { value: ThemeChoice; label: string; hint: string }[] = [
  { value: "system", label: "System", hint: "Follows your device" },
  { value: "light", label: "Light", hint: "Paper" },
  { value: "dark", label: "Dark", hint: "Screening room" },
];

function subscribe(onChange: () => void) {
  window.addEventListener(THEME_EVENT, onChange);
  window.addEventListener("storage", onChange);
  return () => {
    window.removeEventListener(THEME_EVENT, onChange);
    window.removeEventListener("storage", onChange);
  };
}

// The server cannot know the stored choice, so the first render shows no
// option as selected and the real one appears once the page is interactive.
const serverChoice = () => null;

export default function ThemeSelect() {
  const choice = useSyncExternalStore<ThemeChoice | null>(
    subscribe,
    readThemeChoice,
    serverChoice,
  );

  return (
    <div
      role="radiogroup"
      aria-label="Theme"
      className="grid grid-cols-3 gap-2 sm:max-w-md"
    >
      {OPTIONS.map((option) => {
        const selected = choice === option.value;
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={selected}
            onClick={() => applyThemeChoice(option.value)}
            className={`rounded-xl border px-3 py-2.5 text-left transition-colors ${
              selected
                ? "border-accent bg-accent-soft"
                : "border-line bg-surface hover:border-line-strong"
            }`}
          >
            <span className="block text-sm font-medium">{option.label}</span>
            <span className="block text-xs text-muted">{option.hint}</span>
          </button>
        );
      })}
    </div>
  );
}
