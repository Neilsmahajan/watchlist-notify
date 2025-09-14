"use client";

import { useUser } from "@auth0/nextjs-auth0";
import { LoadingSpinner, Button } from "@/components/ui";
import { redirect } from "next/navigation";
import { useState } from "react";

export default function Search() {
  const { user, isLoading } = useUser();
  const [searchQuery, setSearchQuery] = useState("");

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
        <div className="flex gap-4">
          <div className="flex-1">
            <input
              type="text"
              placeholder="Search for movies, TV shows, actors, or directors..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full px-4 py-3 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            />
          </div>
          <Button
            onClick={() => {
              // TODO: Implement search functionality
              console.log("Searching for:", searchQuery);
            }}
            disabled={!searchQuery.trim()}
          >
            Search
          </Button>
        </div>

        {/* Filter Options */}
        <div className="flex flex-wrap gap-2 mt-4">
          <button className="px-3 py-1 text-sm bg-blue-100 text-blue-800 rounded-full hover:bg-blue-200 transition-colors">
            Movies
          </button>
          <button className="px-3 py-1 text-sm bg-gray-100 text-gray-700 rounded-full hover:bg-gray-200 transition-colors">
            TV Shows
          </button>
          <button className="px-3 py-1 text-sm bg-gray-100 text-gray-700 rounded-full hover:bg-gray-200 transition-colors">
            Documentaries
          </button>
          <button className="px-3 py-1 text-sm bg-gray-100 text-gray-700 rounded-full hover:bg-gray-200 transition-colors">
            Trending
          </button>
        </div>
      </div>

      {/* Search Results */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-8">
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
                  onClick={() => setSearchQuery(title)}
                  className="px-3 py-1 text-sm text-blue-600 bg-blue-50 rounded-full hover:bg-blue-100 transition-colors"
                >
                  {title}
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
