import { redirect } from "next/navigation";

// The dashboard is the home page for a signed-in user. This address is kept
// for old links.
export default function DashboardRedirect() {
  redirect("/");
}
