import type { Metadata } from "next";
import Link from "next/link";
import { PageShell } from "@/components/ui";
import { IconMail } from "@/components/icons";

export const metadata: Metadata = {
  title: "Contact",
  description: "Questions, feedback, support and security reports for Watchlist Notify.",
  alternates: { canonical: "/contact" },
};

const ADDRESS = "contact@watchlistnotify.com";

const REASONS = [
  {
    title: "Questions and feedback",
    text: "Anything about how it works, something you would like it to do, or something that annoyed you.",
  },
  {
    title: "Help with your account",
    text: "Trouble signing in, a digest that did not arrive, a title matched to the wrong film, or a request to delete your account and data.",
  },
  {
    title: "Security reports",
    text: (
      <>
        Found a vulnerability? Put{" "}
        <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[13px] text-fg">
          [SECURITY] short summary
        </code>{" "}
        in the subject line, and please allow time for a fix before disclosing it publicly.
      </>
    ),
  },
];

export default function ContactPage() {
  return (
    <PageShell width="narrow">
      <header className="mb-10">
        <p className="eyebrow">Contact</p>
        <h1 className="display mt-4 text-4xl sm:text-6xl">One address for everything.</h1>
        <p className="mt-5 text-lg leading-relaxed text-muted">
          Have a question or feedback? We would love to hear from you. We typically reply
          within 24 to 48 hours on business days.
        </p>
        <a
          href={`mailto:${ADDRESS}`}
          className="mt-7 inline-flex max-w-full items-center gap-3 rounded-full bg-accent px-6 py-3.5 font-medium text-accent-ink transition-colors hover:bg-accent-hover"
        >
          <IconMail className="size-5 shrink-0" />
          <span className="truncate">{ADDRESS}</span>
        </a>
      </header>

      <dl className="divide-y divide-line border-y border-line">
        {REASONS.map((reason) => (
          <div key={reason.title} className="grid gap-1.5 py-6 sm:grid-cols-[14rem_1fr] sm:gap-8">
            <dt className="font-medium">{reason.title}</dt>
            <dd className="leading-relaxed text-muted">{reason.text}</dd>
          </div>
        ))}
      </dl>

      <section className="prose-page mt-14" aria-labelledby="faq">
        <h2 id="faq">Frequently asked questions</h2>

        <h3>How do I change my notification settings?</h3>
        <p>
          Open <Link href="/settings#notifications">Settings</Link> to choose how often the
          digest email arrives, where it goes, or to turn it off.
        </p>

        <h3>Which streaming services are supported?</h3>
        <p>
          Eighteen services in the United States, including Netflix, Hulu, Prime Video,
          Disney+, HBO Max and Apple TV, and free ones such as Tubi and Pluto TV. Choose
          yours in <Link href="/settings#services">Settings</Link>.
        </p>

        <h3>Is Watchlist Notify free to use?</h3>
        <p>
          Yes. Watchlist Notify is free to use during the beta. It keeps your watchlist and
          tells you when something on it becomes available on a service you subscribe to.
        </p>

        <h3>How is my data used?</h3>
        <p>
          Only to provide the service and send you the notifications you asked for. The{" "}
          <Link href="/privacy">Privacy Policy</Link> has the details.
        </p>

        <h3>Can I look before I sign up?</h3>
        <p>
          Yes. The <Link href="/demo">live demo</Link> is a sample watchlist you can filter
          and open without an account.
        </p>
      </section>
    </PageShell>
  );
}
