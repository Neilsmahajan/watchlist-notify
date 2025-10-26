import { Metadata } from "next";

export const metadata: Metadata = {
  title: "Privacy Policy | Watchlist Notify",
  description: "Privacy Policy for Watchlist Notify",
};

export default function PrivacyPage() {
  return (
    <div className="min-h-screen bg-gray-50 dark:bg-slate-950">
      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        {/* Header */}
        <div className="mb-12">
          <h1 className="text-4xl font-bold text-gray-900 dark:text-slate-100 mb-4">
            Privacy Policy
          </h1>
          <p className="text-gray-600 dark:text-slate-400">
            Last updated: January 2025
          </p>
        </div>

        {/* Content */}
        <div className="bg-white dark:bg-slate-900 rounded-lg shadow-md p-8 space-y-8">
          {/* Introduction */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              Introduction
            </h2>
            <p className="text-gray-600 dark:text-slate-400 mb-4">
              Watchlist Notify (&quot;we,&quot; &quot;our,&quot; or
              &quot;us&quot;) is committed to protecting your privacy. This
              Privacy Policy explains how we collect, use, disclose, and
              safeguard your information when you use our service.
            </p>
            <p className="text-gray-600 dark:text-slate-400">
              By using Watchlist Notify, you agree to the collection and use of
              information in accordance with this policy.
            </p>
          </section>

          {/* Information We Collect */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              Information We Collect
            </h2>

            <h3 className="text-xl font-medium text-gray-900 dark:text-slate-100 mb-3">
              Personal Information
            </h3>
            <p className="text-gray-600 dark:text-slate-400 mb-4">
              When you create an account, we collect:
            </p>
            <ul className="list-disc list-inside text-gray-600 dark:text-slate-400 mb-4 space-y-2">
              <li>Email address</li>
              <li>Name</li>
              <li>Profile picture (if provided via OAuth)</li>
              <li>Authentication credentials (managed by Auth0)</li>
            </ul>

            <h3 className="text-xl font-medium text-gray-900 dark:text-slate-100 mb-3">
              Usage Information
            </h3>
            <p className="text-gray-600 dark:text-slate-400 mb-4">
              We collect information about how you use our service:
            </p>
            <ul className="list-disc list-inside text-gray-600 dark:text-slate-400 mb-4 space-y-2">
              <li>Movies and TV shows added to your watchlist</li>
              <li>Streaming services you subscribe to</li>
              <li>Notification preferences and settings</li>
              <li>Search queries and browsing activity within the app</li>
            </ul>

            <h3 className="text-xl font-medium text-gray-900 dark:text-slate-100 mb-3">
              Automatically Collected Information
            </h3>
            <ul className="list-disc list-inside text-gray-600 dark:text-slate-400 space-y-2">
              <li>IP address and device information</li>
              <li>Browser type and version</li>
              <li>Log data and usage statistics</li>
            </ul>
          </section>

          {/* How We Use Your Information */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              How We Use Your Information
            </h2>
            <p className="text-gray-600 dark:text-slate-400 mb-4">
              We use the collected information for the following purposes:
            </p>
            <ul className="list-disc list-inside text-gray-600 dark:text-slate-400 space-y-2">
              <li>
                <strong>Service Delivery:</strong> To provide and maintain our
                service, including watchlist management and availability
                tracking
              </li>
              <li>
                <strong>Notifications:</strong> To send you email notifications
                about content availability based on your preferences
              </li>
              <li>
                <strong>Personalization:</strong> To customize your experience
                and show relevant content
              </li>
              <li>
                <strong>Improvement:</strong> To analyze usage patterns and
                improve our service
              </li>
              <li>
                <strong>Security:</strong> To detect, prevent, and address
                technical issues and security threats
              </li>
              <li>
                <strong>Communication:</strong> To respond to your inquiries and
                provide customer support
              </li>
            </ul>
          </section>

          {/* Data Sharing and Disclosure */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              Data Sharing and Disclosure
            </h2>
            <p className="text-gray-600 dark:text-slate-400 mb-4">
              We do not sell your personal information. We may share your
              information in the following circumstances:
            </p>
            <ul className="list-disc list-inside text-gray-600 dark:text-slate-400 space-y-2">
              <li>
                <strong>Service Providers:</strong> We use third-party services
                including Auth0 (authentication), MongoDB Atlas (database), TMDb
                (content data), and Postmark (email delivery). These providers
                access your data only to perform services on our behalf.
              </li>
              <li>
                <strong>Legal Requirements:</strong> We may disclose your
                information if required by law or in response to valid legal
                requests.
              </li>
              <li>
                <strong>Business Transfers:</strong> In the event of a merger,
                acquisition, or sale of assets, your information may be
                transferred.
              </li>
            </ul>
          </section>

          {/* Third-Party Services */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              Third-Party Services
            </h2>
            <p className="text-gray-600 dark:text-slate-400 mb-4">
              Our service integrates with the following third-party services:
            </p>
            <ul className="list-disc list-inside text-gray-600 dark:text-slate-400 space-y-2">
              <li>
                <strong>Auth0:</strong> Authentication and identity management
              </li>
              <li>
                <strong>TMDb (The Movie Database):</strong> Content metadata and
                streaming availability information
              </li>
              <li>
                <strong>Postmark:</strong> Email delivery service
              </li>
              <li>
                <strong>MongoDB Atlas:</strong> Database hosting
              </li>
              <li>
                <strong>Upstash Redis:</strong> Caching service
              </li>
              <li>
                <strong>Google Cloud Platform:</strong> Infrastructure and
                hosting
              </li>
              <li>
                <strong>Vercel:</strong> Frontend hosting
              </li>
            </ul>
            <p className="text-gray-600 dark:text-slate-400 mt-4">
              Each third-party service has its own privacy policy. We encourage
              you to review their policies.
            </p>
          </section>

          {/* Data Retention */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              Data Retention
            </h2>
            <p className="text-gray-600 dark:text-slate-400">
              We retain your personal information for as long as your account is
              active or as needed to provide you services. If you delete your
              account, we will delete your personal information within 30 days,
              except where we are required to retain it for legal or compliance
              purposes.
            </p>
          </section>

          {/* Your Rights */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              Your Rights
            </h2>
            <p className="text-gray-600 dark:text-slate-400 mb-4">
              You have the following rights regarding your personal data:
            </p>
            <ul className="list-disc list-inside text-gray-600 dark:text-slate-400 space-y-2">
              <li>
                <strong>Access:</strong> Request a copy of the personal
                information we hold about you
              </li>
              <li>
                <strong>Correction:</strong> Update or correct your personal
                information via your profile settings
              </li>
              <li>
                <strong>Deletion:</strong> Request deletion of your account and
                associated data
              </li>
              <li>
                <strong>Opt-Out:</strong> Unsubscribe from email notifications
                at any time through your settings
              </li>
              <li>
                <strong>Data Portability:</strong> Request a copy of your data
                in a portable format
              </li>
            </ul>
            <p className="text-gray-600 dark:text-slate-400 mt-4">
              To exercise these rights, please contact us at{" "}
              <a
                href="mailto:contact@watchlistnotify.com"
                className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
              >
                contact@watchlistnotify.com
              </a>
              .
            </p>
          </section>

          {/* Security */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              Security
            </h2>
            <p className="text-gray-600 dark:text-slate-400">
              We implement appropriate technical and organizational measures to
              protect your personal information, including encryption, secure
              authentication via Auth0, and regular security audits. However, no
              method of transmission over the Internet or electronic storage is
              100% secure. While we strive to protect your data, we cannot
              guarantee absolute security.
            </p>
          </section>

          {/* Cookies and Tracking */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              Cookies and Tracking
            </h2>
            <p className="text-gray-600 dark:text-slate-400">
              We use HTTP-only cookies for authentication and session
              management. These cookies are essential for the service to
              function properly. We do not use third-party tracking cookies or
              advertising cookies.
            </p>
          </section>

          {/* Children's Privacy */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              Children&apos;s Privacy
            </h2>
            <p className="text-gray-600 dark:text-slate-400">
              Our service is not directed to individuals under the age of 13. We
              do not knowingly collect personal information from children. If
              you become aware that a child has provided us with personal
              information, please contact us so we can delete it.
            </p>
          </section>

          {/* Changes to This Policy */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              Changes to This Privacy Policy
            </h2>
            <p className="text-gray-600 dark:text-slate-400">
              We may update our Privacy Policy from time to time. We will notify
              you of any changes by posting the new Privacy Policy on this page
              and updating the &quot;Last updated&quot; date. You are advised to
              review this Privacy Policy periodically for any changes.
            </p>
          </section>

          {/* Contact Us */}
          <section>
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-slate-100 mb-4">
              Contact Us
            </h2>
            <p className="text-gray-600 dark:text-slate-400">
              If you have any questions about this Privacy Policy, please
              contact us:
            </p>
            <p className="text-gray-600 dark:text-slate-400 mt-4">
              Email:{" "}
              <a
                href="mailto:contact@watchlistnotify.com"
                className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
              >
                contact@watchlistnotify.com
              </a>
            </p>
          </section>
        </div>
      </div>
    </div>
  );
}
