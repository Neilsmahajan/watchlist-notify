import type { Metadata, Viewport } from "next";
import { Fraunces, Geist, Geist_Mono } from "next/font/google";
import SiteHeader, { type HeaderUser } from "@/components/SiteHeader";
import SiteFooter from "@/components/SiteFooter";
import { getSessionUser } from "@/lib/auth/session";
import { themeScript } from "@/lib/theme";
import "./globals.css";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

const fraunces = Fraunces({
  variable: "--font-fraunces",
  subsets: ["latin"],
  style: ["normal", "italic"],
  axes: ["opsz"],
});

const SITE_URL = "https://watchlistnotify.com";
const DESCRIPTION =
  "Keep one watchlist and see which of your films and shows are streaming on the services you already pay for. Checked every day, with an email when something arrives.";

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: {
    default: "Watchlist Notify — your watchlist, on the services you have",
    template: "%s · Watchlist Notify",
  },
  description: DESCRIPTION,
  applicationName: "Watchlist Notify",
  openGraph: {
    type: "website",
    siteName: "Watchlist Notify",
    url: SITE_URL,
    title: "Watchlist Notify — your watchlist, on the services you have",
    description: DESCRIPTION,
  },
  twitter: {
    card: "summary_large_image",
    title: "Watchlist Notify",
    description: DESCRIPTION,
  },
};

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#f6f2ea" },
    { media: "(prefers-color-scheme: dark)", color: "#0c0b0a" },
  ],
};

export default async function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  const session = await getSessionUser();
  const user: HeaderUser | null = session
    ? {
        name: typeof session.name === "string" ? session.name : "",
        email: typeof session.email === "string" ? session.email : "",
        picture: typeof session.picture === "string" ? session.picture : "",
      }
    : null;

  return (
    // The inline script sets data-theme on <html> before React loads, which
    // is the one attribute React is told not to compare.
    <html
      lang="en"
      suppressHydrationWarning
      className={`${geistSans.variable} ${geistMono.variable} ${fraunces.variable}`}
    >
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body className="flex min-h-dvh flex-col">
        <a
          href="#main"
          className="sr-only focus:not-sr-only focus:fixed focus:left-3 focus:top-3 focus:z-50 focus:rounded-full focus:bg-accent focus:px-4 focus:py-2 focus:text-sm focus:font-medium focus:text-accent-ink"
        >
          Skip to content
        </a>
        <SiteHeader user={user} />
        <main id="main" className="flex flex-1 flex-col">
          {children}
        </main>
        <SiteFooter signedIn={Boolean(user)} />
      </body>
    </html>
  );
}
