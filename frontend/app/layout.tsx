import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import { Auth0Provider } from "@auth0/nextjs-auth0";
import Navigation from "@/components/Navigation";
import Footer from "@/components/Footer";
import { ThemeProvider } from "@/components/ThemeProvider";
import "./globals.css";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Watchlist Notify",
  description:
    "Track your watchlist across streaming services and get email alerts when your movies and shows become available.",
};

const themeInitializer = `(() => {
  try {
    const storageKey = 'watchlist-notify-theme';
    const root = document.documentElement;
    if (!root) return;
    const stored = window.localStorage.getItem(storageKey);
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    const theme = stored === 'dark' || stored === 'light' ? stored : (prefersDark ? 'dark' : 'light');
    root.dataset.theme = theme;
    if (theme === 'dark') {
      root.classList.add('dark');
      root.style.colorScheme = 'dark';
    } else {
      root.classList.remove('dark');
      root.style.colorScheme = 'light';
    }
  } catch (error) {
    /* no-op */
  }
})();`
  .replace(/\s{2,}/g, " ")
  .trim();

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body
        suppressHydrationWarning
        className={`${geistSans.variable} ${geistMono.variable} antialiased flex flex-col min-h-screen bg-gray-50 text-gray-900 transition-colors duration-200 dark:bg-slate-950 dark:text-slate-100`}
      >
        <script dangerouslySetInnerHTML={{ __html: themeInitializer }} />
        <Auth0Provider>
          <ThemeProvider>
            <Navigation />
            <main className="flex-1 transition-colors duration-200">
              {children}
            </main>
            <Footer />
          </ThemeProvider>
        </Auth0Provider>
      </body>
    </html>
  );
}
