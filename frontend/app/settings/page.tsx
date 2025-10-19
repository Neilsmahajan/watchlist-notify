import SettingsClient from "@/components/pages/SettingsClient";
import { auth0 } from "@/lib/auth0";
import type { SessionUser } from "@/lib/auth/types";
import { redirect } from "next/navigation";

export default async function Settings() {
  try {
    const session = await auth0.getSession();
    const user = session?.user ? (session.user as SessionUser) : null;

    if (!user) {
      redirect("/auth/login");
    }

    return <SettingsClient user={user} />;
  } catch (err) {
    console.error("Settings session lookup failed", err);
    redirect("/auth/login");
  }
}
