"use client";

import { Button } from "@/components/ui";

// What the demo's detail view shows where a real list has its controls.
export default function DemoNote({ href, label }: { href: string; label: string }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <p className="text-sm text-muted">
        This is the demo list, so it cannot be changed. On your own list you set a status
        here, or remove the title.
      </p>
      <Button href={href} size="sm">
        {label}
      </Button>
    </div>
  );
}
