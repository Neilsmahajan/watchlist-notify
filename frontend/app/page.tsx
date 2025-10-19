import HomeDashboardClient from "@/components/pages/HomeDashboardClient";
import HomeLanding from "@/components/pages/HomeLanding";
import { auth0 } from "@/lib/auth0";
import type { SessionUser } from "@/lib/auth/types";

export default async function Home() {
  let user: SessionUser | null = null;

  try {
    const session = await auth0.getSession();
    user = (session?.user ?? null) as SessionUser | null;
  } catch (err) {
    console.error("Home session lookup failed", err);
  }

  if (!user) {
    return <HomeLanding />;
  }

  return <HomeDashboardClient user={user} />;
}
