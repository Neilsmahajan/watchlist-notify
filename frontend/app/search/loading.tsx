export default function SearchLoading() {
  const cards = Array.from({ length: 6 });

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
      <div className="animate-pulse space-y-8">
        <div className="space-y-3">
          <div className="h-8 w-48 rounded-lg bg-gray-200" />
          <div className="h-4 w-72 rounded-lg bg-gray-200" />
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm">
          <div className="flex flex-col gap-4 sm:flex-row">
            <div className="h-12 flex-1 rounded-lg bg-gray-100" />
            <div className="h-12 w-32 rounded-lg bg-gray-100" />
          </div>
          <div className="mt-4 flex flex-wrap items-center gap-2">
            {["Movies", "TV"].map((label) => (
              <div
                key={label}
                className="h-8 w-20 rounded-full border border-gray-100 bg-gray-100"
              />
            ))}
          </div>
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-8 shadow-sm">
          <div className="mb-6 h-4 w-48 rounded-lg bg-gray-100" />
          <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
            {cards.map((_, index) => (
              <div
                key={index}
                className="flex gap-4 rounded-lg border border-gray-200 p-4"
              >
                <div className="h-36 w-24 rounded-md bg-gray-100" />
                <div className="flex flex-1 flex-col space-y-3">
                  <div className="h-5 w-3/4 rounded bg-gray-100" />
                  <div className="h-4 w-1/2 rounded bg-gray-100" />
                  <div className="h-5 w-20 rounded-full bg-gray-100" />
                  <div className="mt-auto h-10 rounded-lg bg-gray-100" />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
