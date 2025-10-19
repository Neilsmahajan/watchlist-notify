import SearchClient from "@/components/pages/SearchClient";
import { auth0 } from "@/lib/auth0";
import type { SessionUser } from "@/lib/auth/types";
import { redirect } from "next/navigation";

export default async function Search() {
  try {
    const session = await auth0.getSession();
    const user = session?.user ? (session.user as SessionUser) : null;

    if (!user) {
      redirect("/auth/login");
    }

    return <SearchClient user={user} />;
  } catch (err) {
    console.error("Search session lookup failed", err);
    redirect("/auth/login");
  }
}
