"use client";

import { useEffect } from "react";
import { Button, PageShell } from "@/components/ui";

export default function ErrorPage({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error(error);
  }, [error]);

  return (
    <PageShell width="narrow" className="py-24 text-center sm:py-32">
      <p className="eyebrow">Something broke</p>
      <h1 className="display mt-5 text-5xl sm:text-6xl">The projector jammed.</h1>
      <p className="mx-auto mt-6 max-w-md text-lg leading-relaxed text-muted">
        This page could not be shown. Trying again usually fixes it.
        {error.digest ? ` Reference: ${error.digest}.` : ""}
      </p>
      <div className="mt-10 flex flex-wrap justify-center gap-3">
        <Button size="lg" onClick={reset}>
          Try again
        </Button>
        <Button href="/" size="lg" variant="secondary">
          Back to the start
        </Button>
      </div>
    </PageShell>
  );
}
