export default function SettingsLoading() {
  const serviceCards = Array.from({ length: 8 });
  const notificationRows = Array.from({ length: 3 });

  return (
    <div className="mx-auto max-w-4xl px-4 py-8 sm:px-6 lg:px-8">
      <div className="space-y-6 animate-pulse">
        <div className="space-y-3">
          <div className="h-8 w-36 rounded-lg bg-gray-200 dark:bg-gray-700" />
          <div className="h-4 w-64 rounded-lg bg-gray-200 dark:bg-gray-700" />
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
          <div className="mb-4 h-5 w-52 rounded bg-gray-100 dark:bg-gray-800" />
          <div className="flex items-center gap-4">
            <div className="h-16 w-16 rounded-full bg-gray-100 dark:bg-gray-800" />
            <div className="space-y-2">
              <div className="h-4 w-40 rounded bg-gray-100 dark:bg-gray-800" />
              <div className="h-4 w-48 rounded bg-gray-100 dark:bg-gray-800" />
            </div>
          </div>
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
          <div className="mb-4 h-5 w-64 rounded bg-gray-100 dark:bg-gray-800" />
          <div className="mb-6 h-4 w-80 rounded bg-gray-100 dark:bg-gray-800" />
          <div className="mb-6 flex items-center justify-between">
            <div className="space-y-2">
              <div className="h-4 w-56 rounded bg-gray-100 dark:bg-gray-800" />
              <div className="h-4 w-48 rounded bg-gray-100 dark:bg-gray-800" />
            </div>
            <div className="h-9 w-24 rounded-lg bg-gray-100 dark:bg-gray-800" />
          </div>
          <div className="grid gap-4 sm:grid-cols-3 lg:grid-cols-4">
            {serviceCards.map((_, index) => (
              <div
                key={index}
                className="rounded-lg border border-gray-200 p-4 dark:border-gray-800 dark:bg-gray-900"
              >
                <div className="mb-3 flex justify-center">
                  <div className="h-16 w-16 rounded-lg bg-gray-100 dark:bg-gray-800" />
                </div>
                <div className="mb-2 h-4 w-28 rounded bg-gray-100 dark:bg-gray-800" />
                <div className="mb-3 h-4 w-24 rounded-full bg-gray-100 dark:bg-gray-800" />
                <div className="mb-3 h-3 w-20 rounded bg-gray-100 dark:bg-gray-800" />
                <div className="h-9 w-full rounded-lg bg-gray-100 dark:bg-gray-800" />
              </div>
            ))}
          </div>
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
          <div className="mb-4 h-5 w-56 rounded bg-gray-100 dark:bg-gray-800" />
          <div className="space-y-4">
            {notificationRows.map((_, index) => (
              <div
                key={index}
                className="flex items-center justify-between gap-4 rounded-lg border border-gray-100 p-4 dark:border-gray-800 dark:bg-gray-900"
              >
                <div className="space-y-2">
                  <div className="h-4 w-44 rounded bg-gray-100 dark:bg-gray-800" />
                  <div className="h-3 w-56 rounded bg-gray-100 dark:bg-gray-800" />
                </div>
                <div className="h-6 w-12 rounded-full bg-gray-100 dark:bg-gray-800" />
              </div>
            ))}
          </div>
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
          <div className="mb-4 h-5 w-40 rounded bg-gray-100 dark:bg-gray-800" />
          <div className="h-10 w-40 rounded-lg bg-gray-100 dark:bg-gray-800" />
        </div>
      </div>
    </div>
  );
}
