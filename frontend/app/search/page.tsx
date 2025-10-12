"use client";

import { useUser } from "@auth0/nextjs-auth0";
import { LoadingSpinner, Button } from "@/components/ui";
import { redirect } from "next/navigation";
import { FormEvent, useState } from "react";

type SearchResult = {
  tmdb_id: number;
  title: string;
  year?: number;
  type: "movie" | "tv";
  poster_url?: string;
};

type SearchResponse = {
  results?: SearchResult[];
  error?: string;
};

export default function Search() {
  const { user, isLoading } = useUser();
  const [searchQuery, setSearchQuery] = useState("");
  const [searchType, setSearchType] = useState<"movie" | "tv">("movie");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [isSearching, setIsSearching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasSearched, setHasSearched] = useState(false);

  // Fetch search results from the backend via our Next.js proxy route.
  const handleSearch = async (queryValue: string) => {
    const trimmed = queryValue.trim();
    if (!trimmed) {
      return;
    }

    setSearchQuery(trimmed);
    setHasSearched(true);
    setIsSearching(true);
    setError(null);
    setResults([]);

    try {
      const params = new URLSearchParams({
        query: trimmed,
        type: searchType,
      });

      const response = await fetch(`/api/search?${params.toString()}`);
      const data = (await response
        .json()
        .catch(() => null)) as SearchResponse | null;

      if (!response.ok) {
        const message = data?.error || "Search failed. Please try again.";
        setError(message);
        setResults([]);
        return;
      }

      const items = data?.results;
      if (Array.isArray(items)) {
        setResults(items);
      } else {
        setResults([]);
      }
    } catch (err) {
      console.error("Search error", err);
      setError("Unable to reach search service. Please retry in a moment.");
    } finally {
      setIsSearching(false);
    }
  };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void handleSearch(searchQuery);
  };

  const handleSuggestionClick = (title: string) => {
    setSearchQuery(title);
    void handleSearch(title);
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  if (!user) {
    redirect("/auth/login");
  }

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-gray-900 mb-2">
          Search Content
        </h1>
        <p className="text-gray-600">
          Find movies and TV shows to add to your watchlist
        </p>
      </div>

      {/* Search Bar */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6 mb-8">
        <form onSubmit={handleSubmit} className="flex gap-4">
          <div className="flex-1">
            <input
              type="text"
              placeholder="Search for movies, TV shows, actors, or directors..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full px-4 py-3 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            />
          </div>
          <Button disabled={!searchQuery.trim()} loading={isSearching}>
            Search
          </Button>
        </form>

        {/* Filter Options */}
        <div className="flex flex-wrap gap-2 mt-4 items-center">
          <span className="text-sm font-medium text-gray-700">
            Search type:
          </span>
          {["movie", "tv"].map((type) => {
            const isActive = searchType === type;
            return (
              <button
                key={type}
                type="button"
                onClick={() => setSearchType(type as "movie" | "tv")}
                className={`px-3 py-1 text-sm rounded-full transition-colors border ${
                  isActive
                    ? "bg-blue-100 border-blue-300 text-blue-800"
                    : "bg-white border-gray-200 text-gray-700 hover:bg-gray-100"
                }`}
                aria-pressed={isActive}
              >
                {type === "movie" ? "Movies" : "TV"}
              </button>
            );
          })}
        </div>
      </div>

      {/* Search Results */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-8">
        {(() => {
          if (isSearching) {
            return (
              <div className="flex flex-col items-center gap-4 text-gray-500">
                <LoadingSpinner size="lg" />
                <p>
                  Searching {searchType === "movie" ? "movies" : "TV shows"}
                  ...
                </p>
              </div>
            );
          }

          if (error) {
            return (
              <div className="text-center text-red-600" role="alert">
                {error}
              </div>
            );
          }

          if (!hasSearched) {
            return (
              <div className="text-center text-gray-500">
                <svg
                  className="w-16 h-16 mx-auto mb-4 text-gray-400"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
                  />
                </svg>
                <h3 className="text-lg font-medium text-gray-900 mb-2">
                  Start your search
                </h3>
                <p className="text-gray-500 mb-6">
                  Enter a movie or TV show title above to begin searching
                </p>

                {/* Popular Suggestions */}
                <div className="max-w-md mx-auto">
                  <h4 className="text-sm font-medium text-gray-700 mb-3">
                    Popular searches:
                  </h4>
                  <div className="flex flex-wrap gap-2 justify-center">
                    {[
                      "The Bear",
                      "Dune",
                      "Stranger Things",
                      "The Office",
                      "Breaking Bad",
                      "Avatar",
                    ].map((title) => (
                      <button
                        key={title}
                        onClick={() => handleSuggestionClick(title)}
                        className="px-3 py-1 text-sm text-blue-600 bg-blue-50 rounded-full hover:bg-blue-100 transition-colors"
                        type="button"
                      >
                        {title}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
            );
          }

          if (results.length === 0) {
            return (
              <div className="text-center text-gray-500">
                No results found for "{searchQuery}". Try checking the spelling
                or using a different title.
              </div>
            );
          }

          return (
            <ul className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
              {results.map((item) => (
                <li
                  key={`${item.type}-${item.tmdb_id}`}
                  className="flex gap-4 rounded-lg border border-gray-200 p-4 shadow-sm"
                >
                  <div className="w-24 flex-shrink-0">
                    {item.poster_url ? (
                      <img
                        src={item.poster_url}
                        alt={`${item.title} poster`}
                        className="h-36 w-24 rounded-md object-cover"
                      />
                    ) : (
                      <div className="flex h-36 w-24 items-center justify-center rounded-md bg-gray-100 text-xs font-medium text-gray-500">
                        No poster
                      </div>
                    )}
                  </div>
                  <div className="flex flex-1 flex-col">
                    <h3 className="text-lg font-semibold text-gray-900">
                      {item.title}
                    </h3>
                    <p className="text-sm text-gray-600">
                      {item.year ? item.year : "Year unknown"}
                    </p>
                    <span className="mt-2 inline-flex w-fit items-center rounded-full bg-blue-50 px-2 py-0.5 text-xs font-medium text-blue-700">
                      {item.type === "movie" ? "Movie" : "TV"}
                    </span>
                    <div className="mt-auto pt-4">
                      <Button variant="outline" disabled className="w-full">
                        Add to Watchlist
                      </Button>
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          );
        })()}
      </div>
    </div>
  );
}
