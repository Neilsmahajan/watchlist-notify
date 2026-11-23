import type { Metadata } from "next";
import ImportSection from "@/components/settings/ImportSection";
import NotificationsSection from "@/components/settings/NotificationsSection";
import Section from "@/components/settings/Section";
import ServicesSection from "@/components/settings/ServicesSection";
import ThemeSelect from "@/components/ThemeSelect";
import { Button, PageHeading, PageShell } from "@/components/ui";
import { getMe, getServices } from "@/lib/api/backend";
import { requireSession } from "@/lib/auth/session";

export const metadata: Metadata = {
  title: "Settings",
  robots: { index: false },
};

// Everything the sections need is fetched here, on the server, so the page
// arrives filled in rather than as four spinners.
export default async function SettingsPage() {
  const { user, token } = await requireSession("/settings");
  const [me, services] = await Promise.all([getMe(token), getServices(token)]);

  const account = me.ok ? me.data : null;
  const email = account?.email || user.email || "";

  return (
    <PageShell width="medium">
      <PageHeading title="Settings">
        Your services, how often you hear from us, and where your list comes from.
      </PageHeading>

      <Section
        id="services"
        title="Your services"
        description="Tick what you can watch on. Each title on your list is checked against these, and the change takes effect at once."
      >
        <ServicesSection
          initial={services.ok ? (services.data.services ?? []) : []}
          initialError={services.ok ? null : services.error}
        />
      </Section>

      <Section
        id="notifications"
        title="Email digest"
        description="A digest lists what on your watchlist is streaming on your services. Choose how often, or leave it off."
      >
        <NotificationsSection
          initial={account}
          initialError={me.ok ? null : me.error}
          accountEmail={email}
        />
      </Section>

      <Section
        id="import"
        title="Bring a list"
        description="Link a public IMDb list to keep in step with it, or import a file once."
      >
        <ImportSection imdbSync={account?.imdb_sync ?? null} />
      </Section>

      <Section
        id="appearance"
        title="Appearance"
        description="Light, dark, or whatever your device is set to. Remembered on this device."
      >
        <ThemeSelect />
      </Section>

      <Section id="account" title="Account">
        <dl className="grid gap-x-8 gap-y-4 text-sm sm:grid-cols-2">
          <div>
            <dt className="slate text-muted">Signed in as</dt>
            <dd className="mt-1 truncate">{email || "Unknown"}</dd>
          </div>
          <div>
            <dt className="slate text-muted">Region</dt>
            <dd className="mt-1">{account?.region || "US"}</dd>
          </div>
        </dl>
        <div className="mt-6 flex flex-wrap gap-2.5">
          <Button href="/profile" variant="secondary">
            View profile
          </Button>
          <Button href="/auth/logout" variant="ghost">
            Sign out
          </Button>
        </div>
        <p className="mt-6 text-sm leading-relaxed text-muted">
          To delete your account and everything stored with it, write to us from the{" "}
          <a href="/contact" className="text-accent-text underline underline-offset-4">
            contact page
          </a>
          .
        </p>
      </Section>
    </PageShell>
  );
}
