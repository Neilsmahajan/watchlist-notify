export default function WatchlistLoading() {
  const skeletonItems = Array.from({ length: 3 });
  const availabilityChips = Array.from({ length: 4 });

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
      <div className="space-y-8 animate-pulse">
        <div className="space-y-3">
          <div className="h-8 w-44 rounded-lg bg-gray-200 dark:bg-gray-700" />
          <div className="h-4 w-72 rounded-lg bg-gray-200 dark:bg-gray-700" />
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
          <div className="flex flex-wrap items-end gap-4">
            <div className="space-y-2">
              <div className="h-4 w-12 rounded bg-gray-100 dark:bg-gray-800" />
              <div className="h-10 w-40 rounded-lg bg-gray-100 dark:bg-gray-800" />
            </div>
            <div className="space-y-2">
              <div className="h-4 w-14 rounded bg-gray-100 dark:bg-gray-800" />
              <div className="h-10 w-48 rounded-lg bg-gray-100 dark:bg-gray-800" />
            </div>
            <div className="ml-auto flex gap-2">
              <div className="h-10 w-24 rounded-lg bg-gray-100 dark:bg-gray-800" />
              <div className="h-10 w-32 rounded-lg bg-gray-100 dark:bg-gray-800" />
              <div className="h-10 w-32 rounded-lg bg-gray-100 dark:bg-gray-800" />
            </div>
          </div>
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
          <div className="mb-4 h-4 w-48 rounded-lg bg-gray-100 dark:bg-gray-800" />
          <div className="space-y-6">
            {skeletonItems.map((_, index) => (
              <div
                key={index}
                className="rounded-lg border border-gray-200 p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900"
              >
                <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                  <div className="space-y-3">
                    <div className="h-6 w-64 rounded bg-gray-100 dark:bg-gray-800" />
                    <div className="flex flex-wrap items-center gap-3">
                      <div className="h-5 w-20 rounded-full bg-gray-100 dark:bg-gray-800" />
                      <div className="h-4 w-12 rounded bg-gray-100 dark:bg-gray-800" />
                      <div className="h-4 w-24 rounded bg-gray-100 dark:bg-gray-800" />
                    </div>
                  </div>
                  <div className="h-10 w-28 rounded-lg bg-gray-100 dark:bg-gray-800" />
                </div>

                <div className="mt-4 grid gap-4 md:grid-cols-2 md:items-center">
                  <div>
                    <div className="mb-2 h-4 w-16 rounded bg-gray-100 dark:bg-gray-800" />
                    <div className="h-10 w-full rounded-lg bg-gray-100 dark:bg-gray-800" />
                  </div>
                  <div className="space-y-2 text-sm">
                    <div className="h-4 w-32 rounded bg-gray-100 dark:bg-gray-800" />
                    <div className="h-4 w-28 rounded bg-gray-100 dark:bg-gray-800" />
                  </div>
                </div>

                <div className="mt-4 rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-gray-800 dark:bg-gray-800">
                  <div className="mb-3 flex flex-wrap items-center gap-2">
                    <div className="h-4 w-48 rounded bg-gray-100 dark:bg-gray-700" />
                    <div className="h-8 w-24 rounded-lg bg-gray-100 dark:bg-gray-700" />
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    {availabilityChips.map((_, chipIndex) => (
                      <div
                        key={chipIndex}
                        className="h-8 w-28 rounded-full bg-gray-100 dark:bg-gray-700"
                      />
                    ))}
                  </div>
                  <div className="mt-3 space-y-2">
                    <div className="h-4 w-56 rounded bg-gray-100 dark:bg-gray-700" />
                    <div className="h-3 w-40 rounded bg-gray-100 dark:bg-gray-700" />
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
