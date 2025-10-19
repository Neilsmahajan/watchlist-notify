import { LoadingSpinner } from "@/components/ui";

export default function RootLoading() {
  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-gradient-to-br from-blue-50 via-white to-indigo-100">
      <div
        className="absolute inset-0 bg-[radial-gradient(circle_at_top,_rgba(79,70,229,0.12),_transparent_55%)]"
        aria-hidden="true"
      />
      <div className="relative flex flex-col items-center gap-4 text-center">
        <LoadingSpinner size="lg" className="text-blue-600" />
        <div>
          <p className="text-lg font-semibold text-gray-900">
            Getting things ready…
          </p>
          <p className="text-sm text-gray-500">
            We are preparing your personalized watchlist experience.
          </p>
        </div>
      </div>
    </div>
  );
}
