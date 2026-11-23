import type { Metadata } from "next";
import { PageShell } from "@/components/ui";

export const metadata: Metadata = {
  title: "Privacy Policy",
  description: "How Watchlist Notify collects, uses and protects your information.",
  alternates: { canonical: "/privacy" },
};

export default function PrivacyPage() {
  return (
    <PageShell width="narrow">
      <header className="mb-10 border-b border-line pb-8">
        <p className="eyebrow">Legal</p>
        <h1 className="display mt-4 text-4xl sm:text-6xl">Privacy Policy</h1>
        <p className="slate mt-5 text-muted">Last updated November 2026</p>
      </header>
      <div className="prose-page">
        
          {/* Introduction */}
          <section>
            <h2>
              Introduction
            </h2>
            <p>
              Watchlist Notify (&quot;we,&quot; &quot;our,&quot; or
              &quot;us&quot;) is committed to protecting your privacy. This
              Privacy Policy explains how we collect, use, disclose, and
              safeguard your information when you use our service.
            </p>
            <p>
              By using Watchlist Notify, you agree to the collection and use of
              information in accordance with this policy.
            </p>
          </section>

          {/* Information We Collect */}
          <section>
            <h2>
              Information We Collect
            </h2>

            <h3>
              Personal Information
            </h3>
            <p>
              When you create an account, we collect:
            </p>
            <ul>
              <li>Email address</li>
              <li>Name</li>
              <li>Profile picture (if provided via OAuth)</li>
              <li>Authentication credentials (managed by Auth0)</li>
            </ul>

            <h3>
              Usage Information
            </h3>
            <p>
              We collect information about how you use our service:
            </p>
            <ul>
              <li>Movies and TV shows added to your watchlist</li>
              <li>Streaming services you subscribe to</li>
              <li>Notification preferences and settings</li>
              <li>Search queries and browsing activity within the app</li>
            </ul>

            <h3>
              Automatically Collected Information
            </h3>
            <ul>
              <li>IP address and device information</li>
              <li>Browser type and version</li>
              <li>Log data and usage statistics</li>
            </ul>
          </section>

          {/* How We Use Your Information */}
          <section>
            <h2>
              How We Use Your Information
            </h2>
            <p>
              We use the collected information for the following purposes:
            </p>
            <ul>
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

          {/* Picks made with AI */}
          <section id="ai" className="scroll-mt-24">
            <h2>
              Picks Made with AI
            </h2>
            <p>
              &quot;What should I watch tonight?&quot; on your home page is
              optional, and nothing is sent anywhere until you press its
              button. When you do, we send Anthropic&apos;s Claude, an AI
              model, what it needs to choose three titles:
            </p>
            <ul>
              <li>
                The titles on your watchlist that are streaming on your
                services, up to 120 of them: each one&apos;s name, year, type,
                running time, genres, rating, the names of your services that
                have it, and the start of its description
              </li>
              <li>What you type or choose as your mood</li>
            </ul>
            <p>
              Your name, email address and account ID are not sent. We do not
              store or log the mood you type or what the model answers. We do
              keep a count of how many picks you ask for each day, to enforce
              a daily limit; each day&apos;s count is deleted within two days.
              Anthropic handles what it receives under its own{" "}
              <a
                href="https://www.anthropic.com/legal/privacy"
                target="_blank"
                rel="noopener noreferrer"
              >
                privacy policy
              </a>
              .
            </p>
          </section>

          {/* Data Sharing and Disclosure */}
          <section>
            <h2>
              Data Sharing and Disclosure
            </h2>
            <p>
              We do not sell your personal information. We may share your
              information in the following circumstances:
            </p>
            <ul>
              <li>
                <strong>Service Providers:</strong> We use third-party services
                including Auth0 (authentication), MongoDB Atlas (database), TMDb
                (content data), Postmark (email delivery), and Anthropic (the
                AI model behind tonight&apos;s picks, only when you ask for
                them). These providers access your data only to perform
                services on our behalf.
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
            <h2>
              Third-Party Services
            </h2>
            <p>
              Our service integrates with the following third-party services:
            </p>
            <ul>
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
                <strong>Anthropic (Claude):</strong> The AI model that chooses
                tonight&apos;s picks, when you ask for them
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
            <p>
              Each third-party service has its own privacy policy. We encourage
              you to review their policies.
            </p>
          </section>

          {/* Data Retention */}
          <section>
            <h2>
              Data Retention
            </h2>
            <p>
              We retain your personal information for as long as your account is
              active or as needed to provide you services. If you delete your
              account, we will delete your personal information within 30 days,
              except where we are required to retain it for legal or compliance
              purposes.
            </p>
          </section>

          {/* Your Rights */}
          <section>
            <h2>
              Your Rights
            </h2>
            <p>
              You have the following rights regarding your personal data:
            </p>
            <ul>
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
            <p>
              To exercise these rights, please contact us at{" "}
              <a
                href="mailto:contact@watchlistnotify.com"
              >
                contact@watchlistnotify.com
              </a>
              .
            </p>
          </section>

          {/* Security */}
          <section>
            <h2>
              Security
            </h2>
            <p>
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
            <h2>
              Cookies and Tracking
            </h2>
            <p>
              We use HTTP-only cookies for authentication and session
              management. These cookies are essential for the service to
              function properly. We do not use third-party tracking cookies or
              advertising cookies.
            </p>
          </section>

          {/* Children's Privacy */}
          <section>
            <h2>
              Children&apos;s Privacy
            </h2>
            <p>
              Our service is not directed to individuals under the age of 13. We
              do not knowingly collect personal information from children. If
              you become aware that a child has provided us with personal
              information, please contact us so we can delete it.
            </p>
          </section>

          {/* Changes to This Policy */}
          <section>
            <h2>
              Changes to This Privacy Policy
            </h2>
            <p>
              We may update our Privacy Policy from time to time. We will notify
              you of any changes by posting the new Privacy Policy on this page
              and updating the &quot;Last updated&quot; date. You are advised to
              review this Privacy Policy periodically for any changes.
            </p>
          </section>

          {/* Contact Us */}
          <section>
            <h2>
              Contact Us
            </h2>
            <p>
              If you have any questions about this Privacy Policy, please
              contact us:
            </p>
            <p>
              Email:{" "}
              <a
                href="mailto:contact@watchlistnotify.com"
              >
                contact@watchlistnotify.com
              </a>
            </p>
          </section>
      </div>
    </PageShell>
  );
}
