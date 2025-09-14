import { LoadingSpinner } from "@/components/ui";

export default function Loading() {
  return (
    <div className="flex items-center justify-center min-h-screen">
      <div className="text-center">
        <LoadingSpinner size="lg" className="mb-4" />
        <p className="text-gray-600">Loading...</p>
      </div>
    </div>
  );
}
