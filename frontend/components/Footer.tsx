import Image from "next/image";

export default function Footer() {
  return (
    <footer className="bg-white border-t border-gray-200 mt-auto">
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
              <span className="text-lg font-bold text-gray-900">
                Watchlist Notify
              </span>
            </div>
            <p className="text-gray-600 text-sm max-w-md">
              Never miss your favorite movies and shows. Track availability
              across all streaming platforms and get notified when content
              becomes available.
            </p>
          </div>

          <div>
            <h3 className="text-sm font-semibold text-gray-900 mb-3">
              Product
            </h3>
            <ul className="space-y-2 text-sm text-gray-600">
              <li>
                <a
                  href="/search"
                  className="hover:text-blue-600 transition-colors"
                >
                  Search
                </a>
              </li>
              <li>
                <a
                  href="/watchlist"
                  className="hover:text-blue-600 transition-colors"
                >
                  Watchlist
                </a>
              </li>
              <li>
                <a
                  href="/settings"
                  className="hover:text-blue-600 transition-colors"
                >
                  Settings
                </a>
              </li>
            </ul>
          </div>

          <div>
            <h3 className="text-sm font-semibold text-gray-900 mb-3">
              Support
            </h3>
            <ul className="space-y-2 text-sm text-gray-600">
              <li>
                <a href="#" className="hover:text-blue-600 transition-colors">
                  Help Center
                </a>
              </li>
              <li>
                <a href="#" className="hover:text-blue-600 transition-colors">
                  Contact Us
                </a>
              </li>
              <li>
                <a href="#" className="hover:text-blue-600 transition-colors">
                  Privacy Policy
                </a>
              </li>
              <li>
                <a href="#" className="hover:text-blue-600 transition-colors">
                  Terms of Service
                </a>
              </li>
            </ul>
          </div>
        </div>

        <div className="border-t border-gray-200 mt-8 pt-6 text-center text-sm text-gray-500">
          <p>&copy; 2025 Watchlist Notify. All rights reserved.</p>
        </div>
      </div>
    </footer>
  );
}
