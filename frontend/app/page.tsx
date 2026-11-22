import Dashboard from "@/components/home/Dashboard";
import Landing, { type WallPoster } from "@/components/home/Landing";
import { getDemoWatchlist } from "@/lib/api/backend";
import { auth0 } from "@/lib/auth0";
import { getSessionUser } from "@/lib/auth/session";
import { SAMPLE_POSTERS } from "@/lib/sample-posters";

// The landing page's wall of posters is the demo list when the API has one
// (cached for five minutes, like the demo itself), and a fixed set of posters
// otherwise. The fixed set carries no service logos: nothing is made up.
async function wallPosters(): Promise<WallPoster[]> {
  const demo = await getDemoWatchlist("available=true&sort=-rating&limit=24");
  const fromDemo = demo.ok
    ? (demo.data.items ?? [])
        .filter((item) => item.poster_path)
        .map((item) => ({
          title: item.title,
          year: item.year,
          poster_path: item.poster_path,
          providers: item.availability?.providers ?? [],
        }))
    : [];
  return fromDemo.length >= 16 ? fromDemo : SAMPLE_POSTERS;
}

export default async function Home() {
  const user = await getSessionUser();
  if (!user) {
    return <Landing wall={await wallPosters()} />;
  }

  // A session without a usable API token falls through to the landing page
  // rather than a redirect loop; signing in again repairs it.
  const access = await auth0.getAccessToken().catch(() => null);
  if (!access?.token) {
    return <Landing wall={await wallPosters()} />;
  }

  return <Dashboard user={user} token={access.token} />;
}
