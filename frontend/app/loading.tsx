import { PageShell, Skeleton } from "@/components/ui";

// Shown while a page's data is on its way from the API.
export default function Loading() {
  return (
    <PageShell>
      <div role="status" aria-label="Loading">
        <Skeleton className="h-3 w-28" />
        <Skeleton className="mt-5 h-12 w-full max-w-xl" />
        <Skeleton className="mt-3 h-12 w-2/3 max-w-md" />
        <div className="mt-12 grid gap-3 sm:grid-cols-3">
          <Skeleton className="h-28 rounded-2xl" />
          <Skeleton className="h-28 rounded-2xl" />
          <Skeleton className="h-28 rounded-2xl" />
        </div>
      </div>
    </PageShell>
  );
}
