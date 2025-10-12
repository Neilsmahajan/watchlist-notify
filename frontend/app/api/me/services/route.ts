import { NextResponse } from "next/server";
import { auth0 } from "@/lib/auth0";

type AppRouteHandler = (
  req: Request,
) => void | Response | Promise<void | Response>;

const authedGet = auth0.withApiAuthRequired(async function handler(
  _req: Request,
) {
  void _req;
  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  const { token } = await auth0.getAccessToken();

  const response = await fetch(`${backend}/api/me/services`, {
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: "application/json",
    },
  });

  const data = await response.json().catch(() => ({}));
  return NextResponse.json(data, { status: response.status });
});

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

  const { token } = await auth0.getAccessToken();

  const response = await fetch(`${backend}/api/me/services`, {
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

export const GET = authedGet as unknown as AppRouteHandler;
export const PATCH = authedPatch as unknown as AppRouteHandler;
