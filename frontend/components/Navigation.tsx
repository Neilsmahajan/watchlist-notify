"use client";

import { useUser } from "@auth0/nextjs-auth0";
import Image from "next/image";
import Link from "next/link";
import { useState } from "react";
import { useTheme } from "@/components/ThemeProvider";

export default function Navigation() {
  const { user, isLoading } = useUser();
  const [isProfileMenuOpen, setIsProfileMenuOpen] = useState(false);
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false);
  const { theme, toggleTheme } = useTheme();
  const isDark = theme === "dark";

  return (
    <nav className="sticky top-0 z-50 border-b border-gray-200 bg-white/80 backdrop-blur-md transition-colors duration-200 dark:border-slate-800 dark:bg-slate-900/80">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex justify-between h-16">
          <div className="flex items-center">
            <Link href="/" className="flex items-center space-x-2">
              <Image
                src="/watchlist_notify_icon_no_background.png"
                alt="Watchlist Notify logo"
                width={32}
                height={32}
                className="w-8 h-8"
              />
              <span className="text-xl font-bold text-gray-900 dark:text-slate-100">
                Watchlist Notify
              </span>
            </Link>
          </div>

          {/* Desktop navigation */}
          <div className="hidden md:flex items-center space-x-8">
            {user && (
              <>
                <Link
                  href="/dashboard"
                  className="text-gray-700 transition-colors hover:text-blue-600 dark:text-slate-200 dark:hover:text-blue-400"
                >
                  Dashboard
                </Link>
                <Link
                  href="/watchlist"
                  className="text-gray-700 transition-colors hover:text-blue-600 dark:text-slate-200 dark:hover:text-blue-400"
                >
                  My Watchlist
                </Link>
                <Link
                  href="/search"
                  className="text-gray-700 transition-colors hover:text-blue-600 dark:text-slate-200 dark:hover:text-blue-400"
                >
                  Search
                </Link>
              </>
            )}

            <div className="flex items-center space-x-4">
              <button
                type="button"
                onClick={toggleTheme}
                aria-label={`Activate ${isDark ? "light" : "dark"} mode`}
                className="rounded-full p-2 text-gray-700 transition-colors hover:bg-gray-100 hover:text-blue-600 dark:text-slate-200 dark:hover:bg-slate-800 dark:hover:text-blue-400"
              >
                {isDark ? (
                  <svg
                    className="h-5 w-5"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth={2}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  >
                    <path d="M12 18a6 6 0 0 1-6-6 6 6 0 0 1 7.2-5.9 1 1 0 0 1 .6 1.8A4 4 0 0 0 12 16a4 4 0 0 0 3.1-6.4 1 1 0 0 1 .6-1.8A6 6 0 0 1 12 18Z" />
                  </svg>
                ) : (
                  <svg
                    className="h-5 w-5"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth={2}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  >
                    <circle cx="12" cy="12" r="4" />
                    <path d="M12 2v2m0 16v2m10-10h-2M4 12H2m17.07 7.07-1.41-1.41M6.34 6.34 4.93 4.93m12.14 0 1.41 1.41M6.34 17.66l-1.41 1.41" />
                  </svg>
                )}
              </button>

              {isLoading ? (
                <div className="h-8 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-slate-700"></div>
              ) : user ? (
                <div className="relative">
                  <button
                    onClick={() => setIsProfileMenuOpen((prev) => !prev)}
                    className="flex items-center space-x-2 text-gray-700 transition-colors hover:text-blue-600 dark:text-slate-200 dark:hover:text-blue-400"
                  >
                    <Image
                      src={user.picture || "/default-avatar.svg"}
                      alt={user.name || "User"}
                      className="w-8 h-8 rounded-full"
                      width={32}
                      height={32}
                    />
                    <span className="hidden sm:block">{user.name}</span>
                  </button>

                  {isProfileMenuOpen && (
                    <div className="absolute right-0 mt-2 w-56 rounded-md bg-white py-1 shadow-lg ring-1 ring-black/10 z-50 dark:bg-slate-900 dark:ring-white/10">
                      <div className="px-4 py-2 text-sm text-gray-500 dark:text-slate-400">
                        {user.email}
                      </div>
                      <hr className="my-1" />
                      <Link
                        href="/profile"
                        className="block px-4 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-100 dark:text-slate-200 dark:hover:bg-slate-800"
                        onClick={() => setIsProfileMenuOpen(false)}
                      >
                        Profile
                      </Link>
                      <Link
                        href="/settings"
                        className="block px-4 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-100 dark:text-slate-200 dark:hover:bg-slate-800"
                        onClick={() => setIsProfileMenuOpen(false)}
                      >
                        Settings
                      </Link>
                      <button
                        type="button"
                        onClick={() => {
                          toggleTheme();
                          setIsProfileMenuOpen(false);
                        }}
                        className="flex w-full items-center justify-between px-4 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-100 dark:text-slate-200 dark:hover:bg-slate-800"
                      >
                        <span>Switch to {isDark ? "light" : "dark"} mode</span>
                        {isDark ? (
                          <svg
                            className="h-4 w-4"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth={2}
                            strokeLinecap="round"
                            strokeLinejoin="round"
                          >
                            <path d="M12 18a6 6 0 0 1-6-6 6 6 0 0 1 7.2-5.9 1 1 0 0 1 .6 1.8A4 4 0 0 0 12 16a4 4 0 0 0 3.1-6.4 1 1 0 0 1 .6-1.8A6 6 0 0 1 12 18Z" />
                          </svg>
                        ) : (
                          <svg
                            className="h-4 w-4"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth={2}
                            strokeLinecap="round"
                            strokeLinejoin="round"
                          >
                            <circle cx="12" cy="12" r="4" />
                            <path d="M12 2v2m0 16v2m10-10h-2M4 12H2m17.07 7.07-1.41-1.41M6.34 6.34 4.93 4.93m12.14 0 1.41 1.41M6.34 17.66l-1.41 1.41" />
                          </svg>
                        )}
                      </button>
                      <hr className="my-1" />
                      <a
                        href="/auth/logout"
                        className="block px-4 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-100 dark:text-slate-200 dark:hover:bg-slate-800"
                      >
                        Sign out
                      </a>
                    </div>
                  )}
                </div>
              ) : (
                <div className="flex items-center space-x-4">
                  <a
                    href="/auth/login"
                    className="text-gray-700 transition-colors hover:text-blue-600 dark:text-slate-200 dark:hover:text-blue-400"
                  >
                    Sign in
                  </a>
                  <a
                    href="/auth/login"
                    className="rounded-lg bg-blue-600 px-4 py-2 text-white transition-colors hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-400"
                  >
                    Get Started
                  </a>
                </div>
              )}
            </div>
          </div>

          {/* Mobile menu button */}
          <div className="md:hidden flex items-center">
            <button
              onClick={() => setIsMobileMenuOpen((prev) => !prev)}
              className="text-gray-700 transition-colors hover:text-blue-600 dark:text-slate-200 dark:hover:text-blue-400"
            >
              <svg
                className="w-6 h-6"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M4 6h16M4 12h16M4 18h16"
                />
              </svg>
            </button>
          </div>
        </div>

        {/* Mobile menu */}
        {isMobileMenuOpen && (
          <div className="md:hidden">
            <div className="px-2 pt-2 pb-3 space-y-1">
              <button
                type="button"
                onClick={() => {
                  toggleTheme();
                  setIsMobileMenuOpen(false);
                }}
                className="block w-full rounded-md px-3 py-2 text-left text-gray-700 transition-colors hover:bg-gray-100 hover:text-blue-600 dark:text-slate-200 dark:hover:bg-slate-800 dark:hover:text-blue-400"
              >
                Switch to {isDark ? "light" : "dark"} mode
              </button>
              {user && (
                <>
                  <Link
                    href="/dashboard"
                    className="block rounded-md px-3 py-2 text-gray-700 transition-colors hover:bg-gray-100 hover:text-blue-600 dark:text-slate-200 dark:hover:bg-slate-800 dark:hover:text-blue-400"
                    onClick={() => setIsMobileMenuOpen(false)}
                  >
                    Dashboard
                  </Link>
                  <Link
                    href="/watchlist"
                    className="block rounded-md px-3 py-2 text-gray-700 transition-colors hover:bg-gray-100 hover:text-blue-600 dark:text-slate-200 dark:hover:bg-slate-800 dark:hover:text-blue-400"
                    onClick={() => setIsMobileMenuOpen(false)}
                  >
                    My Watchlist
                  </Link>
                  <Link
                    href="/search"
                    className="block rounded-md px-3 py-2 text-gray-700 transition-colors hover:bg-gray-100 hover:text-blue-600 dark:text-slate-200 dark:hover:bg-slate-800 dark:hover:text-blue-400"
                    onClick={() => setIsMobileMenuOpen(false)}
                  >
                    Search
                  </Link>
                  <hr className="my-2" />
                  <Link
                    href="/profile"
                    className="block rounded-md px-3 py-2 text-gray-700 transition-colors hover:bg-gray-100 hover:text-blue-600 dark:text-slate-200 dark:hover:bg-slate-800 dark:hover:text-blue-400"
                    onClick={() => setIsMobileMenuOpen(false)}
                  >
                    Profile
                  </Link>
                  <Link
                    href="/settings"
                    className="block rounded-md px-3 py-2 text-gray-700 transition-colors hover:bg-gray-100 hover:text-blue-600 dark:text-slate-200 dark:hover:bg-slate-800 dark:hover:text-blue-400"
                    onClick={() => setIsMobileMenuOpen(false)}
                  >
                    Settings
                  </Link>
                  <a
                    href="/auth/logout"
                    className="block rounded-md px-3 py-2 text-gray-700 transition-colors hover:bg-gray-100 hover:text-blue-600 dark:text-slate-200 dark:hover:bg-slate-800 dark:hover:text-blue-400"
                  >
                    Sign out
                  </a>
                </>
              )}
              {!user && !isLoading && (
                <a
                  href="/auth/login"
                  className="block rounded-md px-3 py-2 text-gray-700 transition-colors hover:bg-gray-100 hover:text-blue-600 dark:text-slate-200 dark:hover:bg-slate-800 dark:hover:text-blue-400"
                >
                  Sign in
                </a>
              )}
            </div>
          </div>
        )}
      </div>
    </nav>
  );
}
