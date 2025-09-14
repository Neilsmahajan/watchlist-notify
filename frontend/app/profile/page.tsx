"use client";

import { useUser } from "@auth0/nextjs-auth0";
import { LoadingSpinner, Button } from "@/components/ui";
import { redirect } from "next/navigation";

export default function Profile() {
  const { user, isLoading } = useUser();

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
    <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-gray-900 mb-2">Profile</h1>
        <p className="text-gray-600">
          View and manage your profile information
        </p>
      </div>

      <div className="space-y-6">
        {/* Profile Header */}
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <div className="flex items-center space-x-6">
            <img
              src={user.picture || "/default-avatar.svg"}
              alt={user.name || "User"}
              className="w-24 h-24 rounded-full"
            />
            <div>
              <h2 className="text-2xl font-bold text-gray-900">{user.name}</h2>
              <p className="text-gray-600 mb-2">{user.email}</p>
              <p className="text-sm text-gray-500">
                Member since{" "}
                {new Date(user.created_at || Date.now()).toLocaleDateString()}
              </p>
            </div>
          </div>
        </div>

        {/* Stats */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6 text-center">
            <div className="text-3xl font-bold text-blue-600 mb-2">0</div>
            <div className="text-gray-600">Items in Watchlist</div>
          </div>
          <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6 text-center">
            <div className="text-3xl font-bold text-green-600 mb-2">0</div>
            <div className="text-gray-600">Items Watched</div>
          </div>
          <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6 text-center">
            <div className="text-3xl font-bold text-purple-600 mb-2">0</div>
            <div className="text-gray-600">Notifications Sent</div>
          </div>
        </div>

        {/* Recent Activity */}
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h3 className="text-lg font-semibold text-gray-900 mb-4">
            Recent Activity
          </h3>
          <div className="text-center py-8">
            <svg
              className="w-12 h-12 mx-auto mb-4 text-gray-400"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M9 5H7a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"
              />
            </svg>
            <p className="text-gray-500 mb-4">No recent activity</p>
            <Button href="/search" size="sm">
              Start Adding Content
            </Button>
          </div>
        </div>

        {/* Preferences */}
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h3 className="text-lg font-semibold text-gray-900 mb-4">
            Preferences
          </h3>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div>
              <h4 className="text-sm font-medium text-gray-900 mb-2">
                Favorite Genres
              </h4>
              <div className="flex flex-wrap gap-2">
                <span className="px-2 py-1 text-xs bg-gray-100 text-gray-700 rounded-full">
                  Not set
                </span>
              </div>
            </div>
            <div>
              <h4 className="text-sm font-medium text-gray-900 mb-2">
                Preferred Languages
              </h4>
              <div className="flex flex-wrap gap-2">
                <span className="px-2 py-1 text-xs bg-gray-100 text-gray-700 rounded-full">
                  English
                </span>
              </div>
            </div>
          </div>
          <div className="mt-4">
            <Button variant="outline" href="/settings">
              Edit Preferences
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
