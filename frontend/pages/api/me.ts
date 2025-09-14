import type { NextApiRequest, NextApiResponse } from "next";
import { getAccessToken, withApiAuthRequired } from "@auth0/nextjs-auth0";

export default withApiAuthRequired(async function handler(
  req: NextApiRequest,
  res: NextApiResponse
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
