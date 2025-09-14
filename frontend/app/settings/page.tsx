"use client";

import { useUser } from "@auth0/nextjs-auth0";
import { LoadingSpinner, Button } from "@/components/ui";
import { redirect } from "next/navigation";
import Image from "next/image";

export default function Settings() {
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
        <h1 className="text-3xl font-bold text-gray-900 mb-2">Settings</h1>
        <p className="text-gray-600">
          Manage your account and notification preferences
        </p>
      </div>

      <div className="space-y-6">
        {/* Profile Section */}
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">
            Profile Information
          </h2>
          <div className="flex items-center space-x-4 mb-6">
            <Image
              src={user.picture || "/default-avatar.svg"}
              alt={user.name || "User"}
              className="w-16 h-16 rounded-full"
              width={64}
              height={64}
            />
            <div>
              <h3 className="text-lg font-medium text-gray-900">{user.name}</h3>
              <p className="text-gray-600">{user.email}</p>
            </div>
          </div>
          <Button variant="outline">Edit Profile</Button>
        </div>

        {/* Streaming Services */}
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">
            Streaming Services
          </h2>
          <p className="text-gray-600 mb-6">
            Connect your streaming services to get personalized availability
            notifications.
          </p>

          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            {[
              { name: "Netflix", icon: "🎬", connected: false },
              { name: "Disney+", icon: "🏰", connected: false },
              { name: "HBO Max", icon: "🎭", connected: false },
              { name: "Amazon Prime", icon: "📦", connected: false },
              { name: "Apple TV+", icon: "🍎", connected: false },
              { name: "Hulu", icon: "🟢", connected: false },
              { name: "Paramount+", icon: "⭐", connected: false },
              { name: "Peacock", icon: "🦚", connected: false },
            ].map((service) => (
              <div
                key={service.name}
                className="border border-gray-200 rounded-lg p-4 text-center hover:bg-gray-50 transition-colors"
              >
                <div className="text-2xl mb-2">{service.icon}</div>
                <div className="text-sm font-medium text-gray-900 mb-2">
                  {service.name}
                </div>
                <Button
                  variant={service.connected ? "secondary" : "outline"}
                  size="sm"
                  className="w-full"
                >
                  {service.connected ? "Connected" : "Connect"}
                </Button>
              </div>
            ))}
          </div>
        </div>

        {/* Notifications */}
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">
            Notifications
          </h2>
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-medium text-gray-900">
                  Email Notifications
                </h3>
                <p className="text-sm text-gray-600">
                  Get notified when content becomes available
                </p>
              </div>
              <button className="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent bg-blue-600 transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-600 focus:ring-offset-2">
                <span className="translate-x-5 pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"></span>
              </button>
            </div>

            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-medium text-gray-900">
                  Weekly Digest
                </h3>
                <p className="text-sm text-gray-600">
                  Get a weekly summary of new content
                </p>
              </div>
              <button className="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent bg-gray-200 transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-600 focus:ring-offset-2">
                <span className="translate-x-0 pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"></span>
              </button>
            </div>

            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-medium text-gray-900">
                  Trending Alerts
                </h3>
                <p className="text-sm text-gray-600">
                  Get notified about trending content in your genres
                </p>
              </div>
              <button className="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent bg-gray-200 transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-600 focus:ring-offset-2">
                <span className="translate-x-0 pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"></span>
              </button>
            </div>
          </div>
        </div>

        {/* Account Actions */}
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">Account</h2>
          <div className="space-y-4">
            <Button variant="outline" className="w-full sm:w-auto">
              Download My Data
            </Button>
            <div className="border-t pt-4">
              <Button
                href="/auth/logout"
                variant="secondary"
                className="w-full sm:w-auto"
              >
                Sign Out
              </Button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
