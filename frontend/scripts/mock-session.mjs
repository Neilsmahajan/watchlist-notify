#!/usr/bin/env node
// Prints a session cookie for a made-up user, so the signed-in pages can be
// looked at locally against scripts/mock-api.mjs without an Auth0 tenant.
// A development tool: the cookie is only valid for the AUTH0_SECRET it was
// made with, which must be a throwaway value, never a real one.
//
//   AUTH0_SECRET=<the dev server's throwaway secret> node scripts/mock-session.mjs
//
// Set the printed value as the `__wl_session` cookie for localhost.

import { generateSessionCookie } from "@auth0/nextjs-auth0/testing";

const secret = process.env.AUTH0_SECRET;
if (!secret) {
  console.error("AUTH0_SECRET is not set");
  process.exit(2);
}

const now = Math.floor(Date.now() / 1000);
const cookie = await generateSessionCookie(
  {
    user: {
      sub: "mock|viewer",
      name: "Sam Viewer",
      email: "viewer@example.com",
      picture: "",
    },
    tokenSet: {
      accessToken: "mock-access-token",
      expiresAt: now + 24 * 3600,
      scope: "openid profile email",
    },
    internal: { sid: "mock-session", createdAt: now },
  },
  { secret },
);

console.log(cookie);
