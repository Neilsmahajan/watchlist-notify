export default function ProfileLoading() {
  const statBlocks = Array.from({ length: 5 });
  const summaryBlocks = Array.from({ length: 3 });
  const serviceRows = Array.from({ length: 4 });

  return (
    <div className="mx-auto max-w-6xl px-4 py-8 sm:px-6 lg:px-8">
      <div className="space-y-6 animate-pulse">
        <div className="space-y-3">
          <div className="h-8 w-40 rounded-lg bg-gray-200 dark:bg-gray-700" />
          <div className="h-4 w-64 rounded-lg bg-gray-200 dark:bg-gray-700" />
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
          <div className="flex flex-col gap-6 md:flex-row md:items-center md:justify-between">
            <div className="flex items-center gap-4">
              <div className="h-24 w-24 rounded-full bg-gray-100 dark:bg-gray-800" />
              <div className="space-y-3">
                <div className="h-6 w-40 rounded bg-gray-100 dark:bg-gray-800" />
                <div className="h-4 w-52 rounded bg-gray-100 dark:bg-gray-800" />
                <div className="h-4 w-32 rounded bg-gray-100 dark:bg-gray-800" />
              </div>
            </div>
            <div className="flex w-full flex-col gap-3 sm:w-auto sm:flex-row">
              <div className="h-10 flex-1 rounded-lg bg-gray-100 dark:bg-gray-800 sm:w-36" />
              <div className="h-10 flex-1 rounded-lg bg-gray-100 dark:bg-gray-800 sm:w-36" />
            </div>
          </div>

          <div className="mt-6 grid gap-4 sm:grid-cols-3">
            {summaryBlocks.map((_, index) => (
              <div
                key={index}
                className="rounded-lg bg-gray-50 px-4 py-3 dark:bg-gray-800"
              >
                <div className="h-3 w-24 rounded bg-gray-200 dark:bg-gray-700" />
                <div className="mt-2 h-6 w-16 rounded bg-gray-200 dark:bg-gray-700" />
              </div>
            ))}
          </div>
        </div>

        <div className="space-y-2">
          <div className="h-3 w-56 rounded bg-gray-100 dark:bg-gray-800" />
          <div className="h-3 w-72 rounded bg-gray-100 dark:bg-gray-800" />
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
          <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
            <div className="space-y-2">
              <div className="h-5 w-48 rounded bg-gray-100 dark:bg-gray-800" />
              <div className="h-4 w-64 rounded bg-gray-100 dark:bg-gray-800" />
            </div>
            <div className="h-9 w-40 rounded-lg bg-gray-100 dark:bg-gray-800" />
          </div>
          <div className="mt-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
            {statBlocks.map((_, index) => (
              <div
                key={index}
                className="rounded-lg border border-gray-200 px-4 py-5 dark:border-gray-800"
              >
                <div className="mb-2 h-8 rounded bg-gray-100 dark:bg-gray-800" />
                <div className="h-3 rounded bg-gray-100 dark:bg-gray-800" />
              </div>
            ))}
          </div>
        </div>

        <div className="grid gap-6 lg:grid-cols-3">
          <div className="space-y-6 lg:col-span-2">
            <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
              <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
                <div className="space-y-2">
                  <div className="h-5 w-48 rounded bg-gray-100 dark:bg-gray-800" />
                  <div className="h-4 w-64 rounded bg-gray-100 dark:bg-gray-800" />
                </div>
                <div className="h-9 w-44 rounded-lg bg-gray-100 dark:bg-gray-800" />
              </div>
              <div className="mt-6 space-y-4">
                {Array.from({ length: 3 }).map((_, index) => (
                  <div
                    key={index}
                    className="flex flex-col gap-4 rounded-lg border border-gray-200 p-4 dark:border-gray-800 md:flex-row md:items-start md:justify-between"
                  >
                    <div className="space-y-3">
                      <div className="flex items-center gap-2">
                        <div className="h-5 w-20 rounded-full bg-gray-100 dark:bg-gray-800" />
                        <div className="h-4 w-12 rounded bg-gray-100 dark:bg-gray-800" />
                        <div className="h-4 w-24 rounded bg-gray-100 dark:bg-gray-800" />
                      </div>
                      <div className="h-5 w-40 rounded bg-gray-100 dark:bg-gray-800" />
                      <div className="h-4 w-28 rounded bg-gray-100 dark:bg-gray-800" />
                    </div>
                    <div className="flex w-full flex-wrap gap-2 md:w-48">
                      {Array.from({ length: 3 }).map((_, providerIndex) => (
                        <div
                          key={providerIndex}
                          className="h-6 flex-1 rounded-full bg-gray-100 dark:bg-gray-800"
                        />
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
              <div className="space-y-3">
                <div className="h-5 w-40 rounded bg-gray-100 dark:bg-gray-800" />
                <div className="h-4 w-full rounded bg-gray-100 dark:bg-gray-800" />
                <div className="h-4 w-5/6 rounded bg-gray-100 dark:bg-gray-800" />
              </div>
            </div>
          </div>

          <div className="space-y-6">
            <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
              <div className="flex items-center justify-between">
                <div className="space-y-2">
                  <div className="h-5 w-44 rounded bg-gray-100 dark:bg-gray-800" />
                  <div className="h-4 w-56 rounded bg-gray-100 dark:bg-gray-800" />
                </div>
                <div className="h-9 w-32 rounded-lg bg-gray-100 dark:bg-gray-800" />
              </div>
              <div className="mt-6 space-y-3">
                {serviceRows.map((_, index) => (
                  <div
                    key={index}
                    className="flex items-center justify-between gap-3 rounded-lg border border-gray-200 px-3 py-3 dark:border-gray-800"
                  >
                    <div className="space-y-2">
                      <div className="h-4 w-32 rounded bg-gray-100 dark:bg-gray-800" />
                      <div className="h-3 w-24 rounded bg-gray-100 dark:bg-gray-800" />
                    </div>
                    <div className="h-6 w-16 rounded-full bg-gray-100 dark:bg-gray-800" />
                  </div>
                ))}
              </div>
            </div>

            <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
              <div className="space-y-3">
                <div className="h-5 w-32 rounded bg-gray-100 dark:bg-gray-800" />
                {Array.from({ length: 4 }).map((_, index) => (
                  <div
                    key={index}
                    className="h-10 w-full rounded-lg bg-gray-100 dark:bg-gray-800"
                  />
                ))}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
