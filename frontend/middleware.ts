import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";
import { auth0 } from "./lib/auth0";

export async function middleware(request: NextRequest) {
  // Mounts /auth/* routes and manages rolling sessions
  try {
    return await auth0.middleware(request);
  } catch {
    // If decryption fails due to stale/old cookies, clear them and proceed.
    const res = NextResponse.next();

    const cookieHeader = request.headers.get("cookie") || "";
    const names = cookieHeader
      .split(";")
      .map((c) => c.split("=")[0].trim())
      .filter(Boolean);

    for (const name of names) {
      if (
        name === "__session" ||
        name === "__wl_session" ||
        name.startsWith("__txn_") ||
        name.startsWith("__wl_txn_")
      ) {
        res.cookies.delete(name);
      }
    }

    return res;
  }
}

export const config = {
  matcher: [
    // Exclude Next.js internals and standard public files; run on everything else
    "/((?!_next/static|_next/image|favicon.ico|sitemap.xml|robots.txt).*)",
  ],
};
