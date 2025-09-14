import { Auth0Client } from "@auth0/nextjs-auth0/server";

// Central Auth0 client instance for App Router
// Configure default audience/scope so getAccessToken() issues an API token.
export const auth0 = new Auth0Client({
  authorizationParameters: {
    scope: process.env.AUTH0_SCOPE ?? "openid profile email",
    ...(process.env.AUTH0_AUDIENCE
      ? { audience: process.env.AUTH0_AUDIENCE }
      : {}),
  },
  // Use a fresh cookie name to avoid trying to decrypt any stale cookies
  session: {
    cookie: {
      name: "__wl_session",
    },
  },
  transactionCookie: {
    // Use a new prefix so any old __txn_* cookies are ignored
    prefix: "__wl_txn_",
  },
});
