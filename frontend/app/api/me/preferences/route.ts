import { NextResponse } from "next/server";
import { auth0 } from "@/lib/auth0";
import { getAccessTokenOrResponse } from "@/lib/auth/api-token";

type AppRouteHandler = (
  req: Request,
) => void | Response | Promise<void | Response>;

const authedPatch = auth0.withApiAuthRequired(async function handler(
  req: Request,
) {
  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  let body: unknown;

  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "invalid json" }, { status: 400 });
  }

  if (!body || typeof body !== "object") {
    return NextResponse.json({ error: "invalid body" }, { status: 400 });
  }

  const access = await getAccessTokenOrResponse();
  if (!access.ok) {
    return access.response;
  }
  const { token } = access;

  const response = await fetch(`${backend}/api/me/preferences`, {
    method: "PATCH",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify(body),
  });

  const data = await response.json().catch(() => ({}));
  return NextResponse.json(data, { status: response.status });
});

export const PATCH = authedPatch as unknown as AppRouteHandler;
