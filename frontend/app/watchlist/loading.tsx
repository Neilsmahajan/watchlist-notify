import { PageShell, PosterGridSkeleton, Skeleton } from "@/components/ui";

export default function WatchlistLoading() {
  return (
    <PageShell>
      <Skeleton className="mb-10 h-12 w-72 max-w-full" />
      <Skeleton className="h-10 w-full max-w-3xl rounded-full" />
      <Skeleton className="mb-10 mt-3 h-9 w-full max-w-xl rounded-full" />
      <PosterGridSkeleton count={14} />
    </PageShell>
  );
}
