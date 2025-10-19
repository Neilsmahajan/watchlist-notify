import { auth0 } from "@/lib/auth0";
import { redirect } from "next/navigation";

export default async function Dashboard() {
  try {
    const session = await auth0.getSession();
    if (!session?.user) {
      redirect("/auth/login");
    }
  } catch (err) {
    console.error("Dashboard session lookup failed", err);
    redirect("/auth/login");
  }

  redirect("/");
}
