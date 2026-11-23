import Link from "next/link";
import Logo from "@/components/Logo";

const GITHUB = "https://github.com/Neilsmahajan/watchlist-notify";

export default function SiteFooter({ signedIn }: { signedIn: boolean }) {
  return (
    <footer
      className={`mt-auto border-t border-line ${signedIn ? "pb-20 md:pb-0" : ""}`}
    >
      <div className="mx-auto grid max-w-[1400px] gap-10 px-4 py-12 sm:px-6 md:grid-cols-[1.4fr_1fr_1fr] lg:px-8">
        <div>
          <Logo />
          <p className="mt-4 max-w-sm text-sm leading-relaxed text-muted">
            One list of what you want to watch, checked every day against the
            streaming services you already pay for.
          </p>
        </div>

        <nav aria-label="Site" className="text-sm">
          <p className="slate mb-3 text-faint">Site</p>
          <ul className="space-y-2 text-muted">
            <li>
              <Link href="/demo" className="hover:text-fg">
                Live demo
              </Link>
            </li>
            <li>
              <Link href="/contact" className="hover:text-fg">
                Contact
              </Link>
            </li>
            <li>
              <Link href="/privacy" className="hover:text-fg">
                Privacy
              </Link>
            </li>
            <li>
              <Link href="/terms" className="hover:text-fg">
                Terms
              </Link>
            </li>
            <li>
              <a href={GITHUB} target="_blank" rel="noopener noreferrer" className="hover:text-fg">
                Source on GitHub
              </a>
            </li>
          </ul>
        </nav>

        <div className="text-sm">
          <p className="slate mb-3 text-faint">Data</p>
          <p className="leading-relaxed text-muted">
            Titles, posters and logos come from{" "}
            <a
              href="https://www.themoviedb.org/"
              target="_blank"
              rel="noopener noreferrer"
              className="text-fg underline decoration-line-strong underline-offset-[3px] hover:decoration-accent"
            >
              TMDB
            </a>
            . Streaming availability is provided by{" "}
            <a
              href="https://www.justwatch.com/"
              target="_blank"
              rel="noopener noreferrer"
              className="text-fg underline decoration-line-strong underline-offset-[3px] hover:decoration-accent"
            >
              JustWatch
            </a>
            .
          </p>
          <p className="mt-3 text-xs leading-relaxed text-faint">
            This product uses the TMDB API but is not endorsed or certified by
            TMDB.
          </p>
        </div>
      </div>
      <div className="border-t border-line">
        <p className="mx-auto max-w-[1400px] px-4 py-5 text-xs text-faint sm:px-6 lg:px-8">
          © Watchlist Notify. Built by Neil Mahajan.
        </p>
      </div>
    </footer>
  );
}
