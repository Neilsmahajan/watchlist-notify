import type { NextApiRequest, NextApiResponse } from "next";
import { getAccessToken, withApiAuthRequired } from "@auth0/nextjs-auth0";

// Next.js 15 expects API routes to return void | Response | Promise<void | Response>.
// Some wrappers (like withApiAuthRequired) can erase the return type to unknown.
// Explicitly cast the wrapped handler to the expected handler signature to satisfy TS.
type ApiRouteHandler = (
  req: NextApiRequest,
  res: NextApiResponse,
) => void | Response | Promise<void | Response>;

const authedHandler = withApiAuthRequired(async function handler(
  req: NextApiRequest,
  res: NextApiResponse,
) {
  const audience = process.env.AUTH0_AUDIENCE;
  const backend = process.env.BACKEND_URL || "http://localhost:8080";

  const { accessToken } = await getAccessToken(req, res, {
    authorizationParams: audience ? { audience } : undefined,
    scopes: ["openid", "profile", "email"],
  });

  const r = await fetch(`${backend}/api/me`, {
    headers: {
      Authorization: `Bearer ${accessToken}`,
      Accept: "application/json",
    },
  });
  const data = await r.json().catch(() => ({}));
  res.status(r.status).json(data);
});

export default authedHandler as unknown as ApiRouteHandler;
