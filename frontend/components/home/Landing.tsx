import Link from "next/link";
import Poster from "@/components/Poster";
import ServiceLogo from "@/components/ServiceLogo";
import { Button } from "@/components/ui";
import { IconArrowRight, IconCheck, IconMail, IconSearch } from "@/components/icons";
import { SERVICE_CODES, serviceName } from "@/lib/services";
import type { Provider } from "@/lib/watchlist/types";

export type WallPoster = {
  title: string;
  year?: number;
  poster_path?: string;
  // Present only when the poster comes from the live demo list.
  providers?: Provider[];
};

const GITHUB = "https://github.com/Neilsmahajan/watchlist-notify";

// The services lit up in the "say what you pay for" illustration.
const EXAMPLE_SERVICES = new Set(["netflix", "max", "hulu", "apple_tv_plus"]);

function WallTile({ poster, sizes }: { poster: WallPoster; sizes: string }) {
  const providers = poster.providers ?? [];
  return (
    <div className="relative">
      <Poster
        path={poster.poster_path}
        title={poster.title}
        year={poster.year}
        sizes={sizes}
        decorative
        className="shadow-lift ring-1 ring-black/10"
      />
      {providers.length > 0 && (
        <span className="absolute inset-x-0 bottom-0 flex gap-1 rounded-b-lg bg-gradient-to-t from-black/85 to-transparent px-1.5 pb-1.5 pt-6">
          {providers.slice(0, 2).map((provider) => (
            <ServiceLogo
              key={provider.code ?? provider.name}
              code={provider.code}
              name={serviceName(provider.code, undefined, provider.name)}
              logoPath={provider.logo_path}
              labelled
              className="size-5 ring-1 ring-white/25"
            />
          ))}
        </span>
      )}
    </div>
  );
}

// The landing page. No gradient hero and no feature cards: the product is a
// wall of posters, so the page is one.
export default function Landing({ wall }: { wall: WallPoster[] }) {
  const columns = [0, 1, 2, 3].map((c) => wall.filter((_, i) => i % 4 === c).slice(0, 5));
  const strip = wall.slice(0, 12);
  // Three titles for the example email, on three different services where the
  // posters come with services, and one more for the search example.
  const withPoster = wall.filter((p) => p.poster_path);
  const mail: WallPoster[] = [];
  for (const poster of withPoster) {
    const code = poster.providers?.[0]?.code;
    if (code && mail.some((m) => m.providers?.[0]?.code === code)) continue;
    mail.push(poster);
    if (mail.length === 3) break;
  }
  const found = withPoster.find((p) => !mail.includes(p)) ?? mail[0];

  return (
    <div className="overflow-x-clip">
      {/* Hero */}
      <section className="relative mx-auto grid max-w-[1400px] gap-x-12 px-4 pt-12 sm:px-6 sm:pt-20 lg:grid-cols-[minmax(0,1fr)_minmax(0,0.95fr)] lg:px-8 lg:pb-8 lg:pt-24">
        <div className="relative z-10 max-w-2xl animate-rise lg:pb-24 lg:pt-6">
          <p className="eyebrow">Your watchlist, meet your subscriptions</p>
          <h1 className="display mt-5 text-[3.05rem] leading-[0.98] sm:text-7xl lg:text-[5.4rem]">
            Know what you can watch{" "}
            <em className="relative whitespace-nowrap text-accent-text">
              tonight
              <svg
                viewBox="0 0 200 12"
                preserveAspectRatio="none"
                aria-hidden="true"
                className="absolute -bottom-1 left-0 h-[0.14em] w-full text-accent"
              >
                <path d="M2 8c40-6 150-8 196-3" fill="none" stroke="currentColor" strokeWidth="4" strokeLinecap="round" />
              </svg>
            </em>
            .
          </h1>
          <p className="mt-7 max-w-xl text-lg leading-relaxed text-muted sm:text-xl">
            Keep one list of the films and series you mean to watch. Watchlist
            Notify checks it every day against the streaming services you
            already pay for, shows what is on them right now, and emails you
            when another title arrives.
          </p>
          <div className="mt-9 flex flex-col gap-3 sm:flex-row">
            <Button href="/demo" size="lg" className="sm:px-8">
              See a live demo
              <IconArrowRight className="size-4" />
            </Button>
            <Button href="/auth/login" size="lg" variant="secondary">
              Create a free account
            </Button>
          </div>
          <p className="slate mt-6 text-faint">
            No sign-up for the demo · 18 US services · Checked daily
          </p>
        </div>

        {/* Wide screens: a tilted wall of posters beside the headline. */}
        <div
          aria-hidden="true"
          className="pointer-events-none relative hidden h-[660px] select-none lg:block"
          style={{
            maskImage: "linear-gradient(to bottom, transparent, black 14%, black 80%, transparent)",
            WebkitMaskImage: "linear-gradient(to bottom, transparent, black 14%, black 80%, transparent)",
          }}
        >
          <div className="absolute -right-10 -top-24 grid w-[640px] rotate-[7deg] grid-cols-4 gap-4 xl:w-[700px]">
            {columns.map((column, c) => (
              <div
                key={c}
                className="flex animate-rise flex-col gap-4"
                style={{ marginTop: [40, 0, 96, 24][c], animationDelay: `${c * 90}ms` }}
              >
                {column.map((poster) => (
                  <WallTile key={poster.title} poster={poster} sizes="170px" />
                ))}
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* Phones: the same wall as one slow-moving strip. */}
      <div
        aria-hidden="true"
        className="pointer-events-none mt-12 select-none overflow-hidden lg:hidden"
        style={{
          maskImage: "linear-gradient(to right, transparent, black 8%, black 92%, transparent)",
          WebkitMaskImage: "linear-gradient(to right, transparent, black 8%, black 92%, transparent)",
        }}
      >
        <div className="flex w-max gap-3 motion-safe:animate-marquee">
          {[...strip, ...strip].map((poster, index) => (
            <div key={index} className="w-[118px] shrink-0 sm:w-[150px]">
              <WallTile poster={poster} sizes="150px" />
            </div>
          ))}
        </div>
      </div>

      {/* How it works */}
      <section className="mx-auto mt-24 max-w-[1400px] px-4 sm:mt-32 sm:px-6 lg:px-8" aria-labelledby="how-heading">
        <p className="eyebrow">How it works</p>
        <h2 id="how-heading" className="display mt-4 max-w-3xl text-4xl sm:text-6xl">
          Three steps, then it runs itself.
        </h2>

        <ol className="mt-14 divide-y divide-line border-y border-line">
          <li className="grid gap-8 py-10 sm:py-14 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)] lg:items-center lg:gap-16">
            <div>
              <p className="font-mono text-sm text-accent-text">01</p>
              <h3 className="display mt-3 text-3xl sm:text-4xl">Add what you want to watch.</h3>
              <p className="mt-4 max-w-md leading-relaxed text-muted">
                Search any film or series and add it in one tap. Or link your
                public IMDb watchlist and it stays in step with what you add
                there. A CSV export works too.
              </p>
            </div>
            <div className="rounded-2xl border border-line bg-surface p-4 shadow-lift sm:p-5" aria-hidden="true">
              <div className="flex h-11 items-center gap-3 rounded-full border border-line bg-bg px-4 text-sm text-muted">
                <IconSearch className="size-4 text-faint" />
                <span className="text-fg">{found?.title}</span>
                <span className="ml-auto h-4 w-px animate-pulse bg-accent" />
              </div>
              <div className="mt-4 flex items-center gap-4">
                <div className="w-14 shrink-0">
                  <Poster path={found?.poster_path} title={found?.title ?? ""} sizes="56px" decorative />
                </div>
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{found?.title}</p>
                  <p className="slate mt-1 text-faint">{found?.year}</p>
                </div>
                <span className="inline-flex h-9 items-center gap-1.5 rounded-full bg-accent px-4 text-sm font-medium text-accent-ink">
                  <IconCheck className="size-4" />
                  Added
                </span>
              </div>
              <div className="mt-4 flex items-center gap-3 border-t border-line pt-4 text-sm text-muted">
                <span className="rounded-md bg-surface-2 px-2 py-1 font-mono text-xs">imdb.com/user/ur…/watchlist</span>
                <span className="ml-auto whitespace-nowrap">synced daily</span>
              </div>
            </div>
          </li>

          <li className="grid gap-8 py-10 sm:py-14 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)] lg:items-center lg:gap-16">
            <div className="lg:order-2">
              <p className="font-mono text-sm text-accent-text">02</p>
              <h3 className="display mt-3 text-3xl sm:text-4xl">Say what you pay for.</h3>
              <p className="mt-4 max-w-md leading-relaxed text-muted">
                Pick your services from eighteen, free ones included. From then
                on every title is sorted into what you can play now and what
                would cost extra.
              </p>
            </div>
            <ul className="grid grid-cols-6 gap-2.5 sm:gap-3 lg:order-1" aria-label="Supported services">
              {SERVICE_CODES.map((code) => {
                const on = EXAMPLE_SERVICES.has(code);
                return (
                  <li key={code} className="relative">
                    <ServiceLogo
                      code={code}
                      name={serviceName(code)}
                      className={`aspect-square size-full ${on ? "ring-2 ring-accent ring-offset-2 ring-offset-bg" : "opacity-45 grayscale"}`}
                    />
                  </li>
                );
              })}
            </ul>
          </li>

          <li className="grid gap-8 py-10 sm:py-14 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)] lg:items-center lg:gap-16">
            <div>
              <p className="font-mono text-sm text-accent-text">03</p>
              <h3 className="display mt-3 text-3xl sm:text-4xl">Hear about it when it lands.</h3>
              <p className="mt-4 max-w-md leading-relaxed text-muted">
                Availability is checked every day. When something on your list
                turns up on one of your services, it goes into your next email
                digest: weekly, daily, or whenever you choose.
              </p>
            </div>
            <div className="rounded-2xl border border-line bg-surface shadow-lift" aria-hidden="true">
              <div className="flex items-center gap-3 border-b border-line px-5 py-4">
                <span className="flex size-9 items-center justify-center rounded-full bg-accent text-accent-ink">
                  <IconMail className="size-[18px]" />
                </span>
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">Now streaming from your watchlist</p>
                  <p className="truncate text-xs text-muted">Watchlist Notify · example email</p>
                </div>
              </div>
              <ul className="divide-y divide-line px-5">
                {mail.map((poster) => {
                  // A service is shown only when the demo list says so.
                  const provider = poster.providers?.[0];
                  const name = provider ? serviceName(provider.code, undefined, provider.name) : null;
                  return (
                    <li key={poster.title} className="flex items-center gap-4 py-3.5">
                      <div className="w-10 shrink-0">
                        <Poster path={poster.poster_path} title={poster.title} sizes="40px" decorative className="rounded-md" />
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium">{poster.title}</p>
                        <p className="slate mt-0.5 text-faint">{poster.year}</p>
                      </div>
                      {provider && name && (
                        <span className="flex items-center gap-2 text-xs text-muted">
                          <span className="hidden sm:inline">{name}</span>
                          <ServiceLogo code={provider.code} name={name} logoPath={provider.logo_path} labelled className="size-7" />
                        </span>
                      )}
                    </li>
                  );
                })}
              </ul>
            </div>
          </li>
        </ol>
      </section>

      {/* What is in it */}
      <section className="mx-auto mt-24 grid max-w-[1400px] gap-x-16 gap-y-10 px-4 sm:mt-32 sm:px-6 lg:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)] lg:px-8" aria-labelledby="inside-heading">
        <div className="lg:sticky lg:top-28 lg:self-start">
          <p className="eyebrow">What you get</p>
          <h2 id="inside-heading" className="display mt-4 text-4xl sm:text-5xl">
            A list that answers “where?” before you ask.
          </h2>
          <div className="mt-8">
            <Button href="/demo" variant="ink" size="lg">
              Try it on the demo list
              <IconArrowRight className="size-4" />
            </Button>
          </div>
        </div>
        <dl className="divide-y divide-line border-y border-line">
          {[
            ["One grid", "Every title as a poster, with the logos of your services that have it."],
            ["Filters you can share", "By service, status, film or series. The address bar holds the view, so the back button works and links stay put."],
            ["The rest of the picture", "Open a title for the plot, runtime and rating, the services you do not have, and where it can be rented or bought."],
            ["Email digests", "A digest on your schedule with what became available. One click to pause."],
            ["IMDb sync and CSV import", "Bring an existing list instead of retyping it."],
            ["Light and dark", "Follows your device, or pick one."],
          ].map(([term, text]) => (
            <div key={term} className="grid gap-1 py-5 sm:grid-cols-[13rem_1fr] sm:gap-6">
              <dt className="font-medium">{term}</dt>
              <dd className="leading-relaxed text-muted">{text}</dd>
            </div>
          ))}
        </dl>
      </section>

      {/* For the people who read source */}
      <section className="mx-auto mt-24 max-w-[1400px] px-4 sm:mt-32 sm:px-6 lg:px-8" aria-labelledby="built-heading">
        <div className="rounded-3xl border border-line bg-surface p-6 sm:p-10">
          <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] lg:gap-16">
            <div>
              <p className="eyebrow">Under the hood</p>
              <h2 id="built-heading" className="display mt-4 text-3xl sm:text-4xl">
                Built end to end, and open to read.
              </h2>
              <p className="mt-4 max-w-md leading-relaxed text-muted">
                A side project by Neil Mahajan, designed, built and run as a
                real service. The code is public.
              </p>
              <div className="mt-6">
                <Button href={GITHUB} variant="secondary">
                  Read the source on GitHub
                </Button>
              </div>
            </div>
            <dl className="grid content-start gap-x-8 gap-y-5 font-mono text-[13px] sm:grid-cols-2">
              {[
                ["API", "Go and Gin on Cloud Run"],
                ["Data", "MongoDB, refreshed by a daily worker"],
                ["Web", "Next.js 15 and React 19 on Vercel"],
                ["Auth", "Auth0, tokens checked by the API"],
                ["Sources", "TMDB for titles, JustWatch for availability"],
                ["Email", "Scheduled digests from a Cloud Run job"],
              ].map(([term, text]) => (
                <div key={term}>
                  <dt className="text-[11px] uppercase tracking-[0.14em] text-faint">{term}</dt>
                  <dd className="mt-1 text-fg">{text}</dd>
                </div>
              ))}
            </dl>
          </div>
        </div>
      </section>

      {/* Close */}
      <section className="mx-auto max-w-3xl px-4 py-24 text-center sm:px-6 sm:py-32">
        <h2 className="display text-5xl sm:text-7xl">
          Stop renting what you already <em className="text-accent-text">have</em>.
        </h2>
        <div className="mt-10 flex flex-col justify-center gap-3 sm:flex-row">
          <Button href="/demo" size="lg">
            See a live demo
            <IconArrowRight className="size-4" />
          </Button>
          <Button href="/auth/login" size="lg" variant="secondary">
            Create a free account
          </Button>
        </div>
        <p className="mt-6 text-sm text-muted">
          Already have one?{" "}
          <Link href="/auth/login" prefetch={false} className="text-accent-text underline-offset-4 hover:underline">
            Sign in
          </Link>
        </p>
      </section>
    </div>
  );
}
