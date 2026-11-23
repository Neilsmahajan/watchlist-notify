import { redirect } from "next/navigation";
import { auth0 } from "@/lib/auth0";
import type { SessionUser } from "@/lib/auth/types";

// The signed-in user, or null. Never throws: a cookie that cannot be read is
// the same as no session.
export async function getSessionUser(): Promise<SessionUser | null> {
  try {
    const session = await auth0.getSession();
    return (session?.user as SessionUser | undefined) ?? null;
  } catch (error) {
    console.error("Session lookup failed", error);
    return null;
  }
}

// For pages that need an account: the user and an API token, or a redirect
// to the login page that comes back here afterwards.
export async function requireSession(
  returnTo: string,
): Promise<{ user: SessionUser; token: string }> {
  const login = `/auth/login?returnTo=${encodeURIComponent(returnTo)}`;
  const user = await getSessionUser();
  if (!user) redirect(login);

  const access = await auth0.getAccessToken().catch((error: unknown) => {
    console.error("Access token lookup failed", error);
    return null;
  });
  if (!access?.token) redirect(login);

  return { user, token: access.token };
}
