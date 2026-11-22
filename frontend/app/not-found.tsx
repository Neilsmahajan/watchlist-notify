import type { Metadata } from "next";
import { Button, PageShell } from "@/components/ui";

export const metadata: Metadata = { title: "Not found" };

export default function NotFound() {
  return (
    <PageShell width="narrow" className="py-24 text-center sm:py-32">
      <p className="eyebrow">Error 404</p>
      <h1 className="display mt-5 text-5xl sm:text-7xl">
        This reel is <em className="text-accent-text">missing</em>.
      </h1>
      <p className="mx-auto mt-6 max-w-md text-lg leading-relaxed text-muted">
        There is no page at this address. It may have moved, or the link may be mistyped.
      </p>
      <div className="mt-10 flex flex-wrap justify-center gap-3">
        <Button href="/" size="lg">
          Back to the start
        </Button>
        <Button href="/demo" size="lg" variant="secondary">
          See the demo
        </Button>
      </div>
    </PageShell>
  );
}
