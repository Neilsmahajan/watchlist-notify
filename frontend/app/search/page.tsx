import type { Metadata } from "next";
import { Suspense } from "react";
import SearchClient from "@/components/pages/SearchClient";
import { PageHeading, PageShell } from "@/components/ui";
import { getSessionUser } from "@/lib/auth/session";
import { redirect } from "next/navigation";

export const metadata: Metadata = {
  title: "Search",
  robots: { index: false },
};

export default async function SearchPage() {
  if (!(await getSessionUser())) redirect("/auth/login?returnTo=%2Fsearch");

  return (
    <PageShell>
      <PageHeading title="Find something to watch">
        Search films and series, and add them to your watchlist.
      </PageHeading>
      <Suspense>
        <SearchClient />
      </Suspense>
    </PageShell>
  );
}
