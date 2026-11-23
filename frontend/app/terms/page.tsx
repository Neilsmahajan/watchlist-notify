import type { Metadata } from "next";
import { PageShell } from "@/components/ui";

export const metadata: Metadata = {
  title: "Terms of Service",
  description: "The terms that apply when you use Watchlist Notify.",
  alternates: { canonical: "/terms" },
};

export default function TermsPage() {
  return (
    <PageShell width="narrow">
      <header className="mb-10 border-b border-line pb-8">
        <p className="eyebrow">Legal</p>
        <h1 className="display mt-4 text-4xl sm:text-6xl">Terms of Service</h1>
        <p className="slate mt-5 text-muted">Last updated January 2025</p>
      </header>
      <div className="prose-page">
        
          {/* Introduction */}
          <section>
            <h2>
              Agreement to Terms
            </h2>
            <p>
              These Terms of Service (&quot;Terms&quot;) govern your access to
              and use of Watchlist Notify&apos;s website, services, and
              applications (collectively, the &quot;Service&quot;). By accessing
              or using the Service, you agree to be bound by these Terms.
            </p>
            <p>
              If you do not agree to these Terms, please do not use the Service.
            </p>
          </section>

          {/* Service Description */}
          <section>
            <h2>
              Service Description
            </h2>
            <p>
              Watchlist Notify is a service that helps you:
            </p>
            <ul>
              <li>
                Track movies and TV shows you want to watch in a personal
                watchlist
              </li>
              <li>Monitor streaming availability across multiple platforms</li>
              <li>
                Receive email notifications when content becomes available on
                your subscribed services
              </li>
              <li>
                Search and discover content with metadata from The Movie
                Database (TMDb)
              </li>
            </ul>
          </section>

          {/* User Accounts */}
          <section>
            <h2>
              User Accounts
            </h2>
            <p>
              To use the Service, you must create an account. You agree to:
            </p>
            <ul>
              <li>
                Provide accurate, current, and complete information during
                registration
              </li>
              <li>Maintain and promptly update your account information</li>
              <li>Maintain the security of your account credentials</li>
              <li>
                Accept responsibility for all activities that occur under your
                account
              </li>
              <li>
                Notify us immediately of any unauthorized use of your account
              </li>
            </ul>
            <p>
              You must be at least 13 years old to use the Service.
            </p>
          </section>

          {/* Acceptable Use */}
          <section>
            <h2>
              Acceptable Use
            </h2>
            <p>
              You agree not to:
            </p>
            <ul>
              <li>Use the Service for any illegal or unauthorized purpose</li>
              <li>
                Violate any laws in your jurisdiction (including but not limited
                to copyright laws)
              </li>
              <li>
                Attempt to gain unauthorized access to the Service or its
                systems
              </li>
              <li>Interfere with or disrupt the Service or servers</li>
              <li>Use automated systems (bots, scrapers) without permission</li>
              <li>Transmit any viruses, malware, or harmful code</li>
              <li>Impersonate any person or entity</li>
              <li>Harass, abuse, or harm other users</li>
              <li>
                Collect or store personal data about other users without consent
              </li>
            </ul>
          </section>

          {/* Intellectual Property */}
          <section>
            <h2>
              Intellectual Property
            </h2>

            <h3>
              Our Content
            </h3>
            <p>
              The Service and its original content, features, and functionality
              are owned by Watchlist Notify and are protected by international
              copyright, trademark, patent, trade secret, and other intellectual
              property laws.
            </p>

            <h3>
              Third-Party Content
            </h3>
            <p>
              Content metadata and images are provided by The Movie Database
              (TMDb). This product uses the TMDb API but is not endorsed or
              certified by TMDb. All movie and TV show data, including titles,
              descriptions, images, and streaming availability information, are
              owned by their respective copyright holders.
            </p>

            <h3>
              Your Content
            </h3>
            <p>
              You retain all rights to your watchlist data. By using the
              Service, you grant us a limited license to use, store, and display
              your data solely to provide the Service to you.
            </p>
          </section>

          {/* Service Availability */}
          <section>
            <h2>
              Service Availability
            </h2>
            <p>
              We strive to provide a reliable service, but we do not guarantee:
            </p>
            <ul>
              <li>Uninterrupted or error-free operation</li>
              <li>
                Accuracy of streaming availability information (data is provided
                by TMDb and streaming services)
              </li>
              <li>That the Service will meet your specific requirements</li>
              <li>
                That defects will be corrected within a specific timeframe
              </li>
            </ul>
            <p>
              We reserve the right to modify, suspend, or discontinue the
              Service at any time with or without notice.
            </p>
          </section>

          {/* Beta Status */}
          <section>
            <h2>
              Beta Status
            </h2>
            <p>
              Watchlist Notify is currently in beta (v0.9). The Service may
              contain bugs, errors, or incomplete features. We appreciate your
              feedback and patience as we continue to improve the Service. Beta
              features may change or be removed without notice.
            </p>
          </section>

          {/* Email Notifications */}
          <section>
            <h2>
              Email Notifications
            </h2>
            <p>
              By enabling digest email notifications, you consent to receive
              periodic emails about your watchlist availability. You can adjust
              notification frequency or unsubscribe at any time through your
              settings. We will also send occasional service-related emails
              (e.g., account security, important updates) that you cannot opt
              out of.
            </p>
          </section>

          {/* Disclaimer of Warranties */}
          <section>
            <h2>
              Disclaimer of Warranties
            </h2>
            <p>
              THE SERVICE IS PROVIDED &quot;AS IS&quot; AND &quot;AS
              AVAILABLE&quot; WITHOUT WARRANTIES OF ANY KIND, EITHER EXPRESS OR
              IMPLIED, INCLUDING BUT NOT LIMITED TO IMPLIED WARRANTIES OF
              MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, AND
              NON-INFRINGEMENT.
            </p>
            <p>
              We do not warrant that the Service will be uninterrupted, secure,
              or error-free, or that any defects will be corrected.
            </p>
          </section>

          {/* Limitation of Liability */}
          <section>
            <h2>
              Limitation of Liability
            </h2>
            <p>
              TO THE MAXIMUM EXTENT PERMITTED BY LAW, WATCHLIST NOTIFY SHALL NOT
              BE LIABLE FOR ANY INDIRECT, INCIDENTAL, SPECIAL, CONSEQUENTIAL, OR
              PUNITIVE DAMAGES, OR ANY LOSS OF PROFITS OR REVENUES, WHETHER
              INCURRED DIRECTLY OR INDIRECTLY, OR ANY LOSS OF DATA, USE,
              GOODWILL, OR OTHER INTANGIBLE LOSSES RESULTING FROM:
            </p>
            <ul>
              <li>Your use or inability to use the Service</li>
              <li>
                Any unauthorized access to or use of our servers and/or any
                personal information stored therein
              </li>
              <li>Any interruption or cessation of the Service</li>
              <li>
                Any bugs, viruses, or harmful code transmitted through the
                Service
              </li>
              <li>
                Any errors or omissions in content or for any loss or damage
                incurred as a result of your use of any content
              </li>
            </ul>
          </section>

          {/* Indemnification */}
          <section>
            <h2>
              Indemnification
            </h2>
            <p>
              You agree to indemnify, defend, and hold harmless Watchlist Notify
              and its officers, directors, employees, and agents from and
              against any claims, liabilities, damages, losses, and expenses,
              including reasonable attorneys&apos; fees, arising out of or in
              any way connected with your access to or use of the Service or
              your violation of these Terms.
            </p>
          </section>

          {/* Open Source License */}
          <section>
            <h2>
              Open Source License
            </h2>
            <p>
              Watchlist Notify is open source software licensed under the MIT
              License. The source code is available for review, modification,
              and distribution subject to the terms of the MIT License. See the
              LICENSE file in the project repository for full details.
            </p>
          </section>

          {/* Termination */}
          <section>
            <h2>
              Termination
            </h2>
            <p>
              We reserve the right to:
            </p>
            <ul>
              <li>
                Suspend or terminate your access to the Service at any time for
                any reason, including violation of these Terms
              </li>
              <li>Refuse service to anyone for any reason at any time</li>
            </ul>
            <p>
              You may terminate your account at any time by contacting us. Upon
              termination, your right to use the Service will immediately cease.
            </p>
          </section>

          {/* Changes to Terms */}
          <section>
            <h2>
              Changes to Terms
            </h2>
            <p>
              We reserve the right to modify these Terms at any time. If we make
              material changes, we will notify you by email or through a notice
              on the Service. Your continued use of the Service after such
              modifications constitutes your acceptance of the updated Terms.
            </p>
          </section>

          {/* Governing Law */}
          <section>
            <h2>
              Governing Law
            </h2>
            <p>
              These Terms shall be governed by and construed in accordance with
              the laws of the United States, without regard to its conflict of
              law provisions. Any disputes arising from these Terms or the
              Service shall be resolved in the courts of competent jurisdiction.
            </p>
          </section>

          {/* Severability */}
          <section>
            <h2>
              Severability
            </h2>
            <p>
              If any provision of these Terms is held to be invalid or
              unenforceable, the remaining provisions will continue in full
              force and effect.
            </p>
          </section>

          {/* Contact Information */}
          <section>
            <h2>
              Contact Us
            </h2>
            <p>
              If you have any questions about these Terms, please contact us:
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

          {/* Acknowledgment */}
          <section>
            <p>
              By using Watchlist Notify, you acknowledge that you have read,
              understood, and agree to be bound by these Terms of Service.
            </p>
          </section>
      </div>
    </PageShell>
  );
}
