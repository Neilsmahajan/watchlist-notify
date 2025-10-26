import { Metadata } from "next";

export const metadata: Metadata = {
  title: "Contact Us | Watchlist Notify",
  description: "Get in touch with the Watchlist Notify team",
};

export default function ContactPage() {
  return (
    <div className="min-h-screen bg-gray-50 dark:bg-slate-950">
      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        {/* Header */}
        <div className="text-center mb-12">
          <h1 className="text-4xl font-bold text-gray-900 dark:text-slate-100 mb-4">
            Contact Us
          </h1>
          <p className="text-lg text-gray-600 dark:text-slate-400">
            Have a question or feedback? We&apos;d love to hear from you.
          </p>
        </div>

        {/* Contact Information */}
        <div className="bg-white dark:bg-slate-900 rounded-lg shadow-md p-8 mb-8">
          <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-6">
            Get in Touch
          </h2>

          <div className="space-y-6">
            {/* Email */}
            <div>
              <h3 className="text-lg font-medium text-gray-900 dark:text-slate-100 mb-2">
                General Inquiries
              </h3>
              <p className="text-gray-600 dark:text-slate-400 mb-2">
                For general questions, feedback, or support requests:
              </p>
              <a
                href="mailto:contact@watchlistnotify.com"
                className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300 font-medium"
              >
                contact@watchlistnotify.com
              </a>
            </div>

            {/* Support */}
            <div>
              <h3 className="text-lg font-medium text-gray-900 dark:text-slate-100 mb-2">
                Technical Support
              </h3>
              <p className="text-gray-600 dark:text-slate-400 mb-2">
                Experiencing issues or need help with your account? Reach out to
                our support team:
              </p>
              <a
                href="mailto:contact@watchlistnotify.com"
                className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300 font-medium"
              >
                contact@watchlistnotify.com
              </a>
            </div>

            {/* Security */}
            <div>
              <h3 className="text-lg font-medium text-gray-900 dark:text-slate-100 mb-2">
                Security Issues
              </h3>
              <p className="text-gray-600 dark:text-slate-400 mb-2">
                If you&apos;ve discovered a security vulnerability, please email
                us with subject line{" "}
                <code className="bg-gray-100 dark:bg-slate-800 px-2 py-1 rounded text-sm">
                  [SECURITY] &lt;short summary&gt;
                </code>
              </p>
              <a
                href="mailto:contact@watchlistnotify.com"
                className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300 font-medium"
              >
                contact@watchlistnotify.com
              </a>
              <p className="text-sm text-gray-500 dark:text-slate-500 mt-2">
                Please allow time for remediation before public disclosure.
              </p>
            </div>
          </div>
        </div>

        {/* FAQ Section */}
        <div className="bg-white dark:bg-slate-900 rounded-lg shadow-md p-8">
          <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-6">
            Frequently Asked Questions
          </h2>

          <div className="space-y-6">
            <div>
              <h3 className="text-lg font-medium text-gray-900 dark:text-slate-100 mb-2">
                How do I change my notification settings?
              </h3>
              <p className="text-gray-600 dark:text-slate-400">
                Visit your{" "}
                <a
                  href="/settings"
                  className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  Settings
                </a>{" "}
                page to customize your digest email frequency and notification
                preferences.
              </p>
            </div>

            <div>
              <h3 className="text-lg font-medium text-gray-900 dark:text-slate-100 mb-2">
                Which streaming services are supported?
              </h3>
              <p className="text-gray-600 dark:text-slate-400">
                We support all major streaming platforms including Netflix,
                Hulu, Prime Video, Disney+, Max, Apple TV+, and many more. You
                can select your subscriptions in your{" "}
                <a
                  href="/settings"
                  className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  Settings
                </a>
                .
              </p>
            </div>

            <div>
              <h3 className="text-lg font-medium text-gray-900 dark:text-slate-100 mb-2">
                Is Watchlist Notify free to use?
              </h3>
              <p className="text-gray-600 dark:text-slate-400">
                Yes! Watchlist Notify is currently free to use during beta. We
                help you track your watchlist and notify you when content
                becomes available on your subscribed services.
              </p>
            </div>

            <div>
              <h3 className="text-lg font-medium text-gray-900 dark:text-slate-100 mb-2">
                How is my data used?
              </h3>
              <p className="text-gray-600 dark:text-slate-400">
                We only use your data to provide the service and send you
                notifications about your watchlist. See our{" "}
                <a
                  href="/privacy"
                  className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  Privacy Policy
                </a>{" "}
                for more details.
              </p>
            </div>
          </div>
        </div>

        {/* Response Time Notice */}
        <div className="mt-8 text-center">
          <p className="text-sm text-gray-500 dark:text-slate-500">
            We typically respond to inquiries within 24-48 hours during business
            days.
          </p>
        </div>
      </div>
    </div>
  );
}
