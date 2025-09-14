import { NextResponse } from "next/server";
import { auth0 } from "@/lib/auth0";

type AppRouteHandler = (
  req: Request,
) => void | Response | Promise<void | Response>;

const authed = auth0.withApiAuthRequired(async function handler() {
  const backend = process.env.BACKEND_URL || "http://localhost:8080";

  // Uses default audience/scope from lib/auth0.ts
  const { token } = await auth0.getAccessToken();

  const r = await fetch(`${backend}/api/me`, {
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: "application/json",
    },
  });
  const data = await r.json().catch(() => ({}));
  return NextResponse.json(data, { status: r.status });
});

export const GET = authed as unknown as AppRouteHandler;
