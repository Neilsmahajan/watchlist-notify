import ProfileClient from "@/components/pages/ProfileClient";
import { auth0 } from "@/lib/auth0";
import type { SessionUser } from "@/lib/auth/types";
import { redirect } from "next/navigation";

export default async function Profile() {
  try {
    const session = await auth0.getSession();
    const user = session?.user ? (session.user as SessionUser) : null;

    if (!user) {
      redirect("/auth/login");
    }

    return <ProfileClient user={user} />;
  } catch (err) {
    console.error("Profile session lookup failed", err);
    redirect("/auth/login");
  }
}
