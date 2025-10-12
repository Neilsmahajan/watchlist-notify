import Image from "next/image";

export default function Footer() {
  return (
    <footer className="mt-auto border-t border-gray-200 bg-white transition-colors duration-200 dark:border-slate-800 dark:bg-slate-900">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        <div className="grid grid-cols-1 md:grid-cols-4 gap-8">
          <div className="md:col-span-2">
            <div className="flex items-center space-x-2 mb-4">
              <Image
                src="/watchlist_notify_icon_no_background.png"
                alt="Watchlist Notify logo"
                width={24}
                height={24}
                className="w-6 h-6"
              />
              <span className="text-lg font-bold text-gray-900 dark:text-slate-100">
                Watchlist Notify
              </span>
            </div>
            <p className="text-sm text-gray-600 max-w-md dark:text-slate-400">
              Never miss your favorite movies and shows. Track availability
              across all streaming platforms and get notified when content
              becomes available.
            </p>
          </div>

          <div>
            <h3 className="text-sm font-semibold text-gray-900 mb-3 dark:text-slate-100">
              Product
            </h3>
            <ul className="space-y-2 text-sm text-gray-600 dark:text-slate-400">
              <li>
                <a
                  href="/search"
                  className="transition-colors hover:text-blue-600 dark:hover:text-blue-400"
                >
                  Search
                </a>
              </li>
              <li>
                <a
                  href="/watchlist"
                  className="transition-colors hover:text-blue-600 dark:hover:text-blue-400"
                >
                  Watchlist
                </a>
              </li>
              <li>
                <a
                  href="/settings"
                  className="transition-colors hover:text-blue-600 dark:hover:text-blue-400"
                >
                  Settings
                </a>
              </li>
            </ul>
          </div>

          <div>
            <h3 className="text-sm font-semibold text-gray-900 mb-3 dark:text-slate-100">
              Support
            </h3>
            <ul className="space-y-2 text-sm text-gray-600 dark:text-slate-400">
              <li>
                <a
                  href="#"
                  className="transition-colors hover:text-blue-600 dark:hover:text-blue-400"
                >
                  Help Center
                </a>
              </li>
              <li>
                <a
                  href="#"
                  className="transition-colors hover:text-blue-600 dark:hover:text-blue-400"
                >
                  Contact Us
                </a>
              </li>
              <li>
                <a
                  href="#"
                  className="transition-colors hover:text-blue-600 dark:hover:text-blue-400"
                >
                  Privacy Policy
                </a>
              </li>
              <li>
                <a
                  href="#"
                  className="transition-colors hover:text-blue-600 dark:hover:text-blue-400"
                >
                  Terms of Service
                </a>
              </li>
            </ul>
          </div>
        </div>

        <div className="border-t border-gray-200 mt-8 pt-6 text-center text-sm text-gray-500 dark:border-slate-800 dark:text-slate-500">
          <p>&copy; 2025 Watchlist Notify. All rights reserved.</p>
        </div>
      </div>
    </footer>
  );
}
