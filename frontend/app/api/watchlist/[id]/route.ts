import { NextResponse } from "next/server";
import { auth0 } from "@/lib/auth0";

type AppRouteHandler = (
  req: Request,
) => void | Response | Promise<void | Response>;

const getIdFromRequest = (req: Request): string | null => {
  try {
    const url = new URL(req.url);
    const segments = url.pathname.split("/").filter(Boolean);
    return segments.pop() ?? null;
  } catch {
    return null;
  }
};

const authedPatch = auth0.withApiAuthRequired(async function handler(
  req: Request,
) {
  const id = getIdFromRequest(req);
  if (!id) {
    return NextResponse.json({ error: "id required" }, { status: 400 });
  }

  let body: unknown;
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "invalid json" }, { status: 400 });
  }

  if (!body || typeof body !== "object") {
    return NextResponse.json({ error: "invalid body" }, { status: 400 });
  }

  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  const { token } = await auth0.getAccessToken();

  const response = await fetch(`${backend}/api/watchlist/${id}`, {
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

const authedDelete = auth0.withApiAuthRequired(async function handler(
  req: Request,
) {
  const id = getIdFromRequest(req);
  if (!id) {
    return NextResponse.json({ error: "id required" }, { status: 400 });
  }

  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  const { token } = await auth0.getAccessToken();

  const response = await fetch(`${backend}/api/watchlist/${id}`, {
    method: "DELETE",
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: "application/json",
    },
  });

  const text = await response.text();
  let data: unknown = {};
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = {};
    }
  }

  return NextResponse.json(data, { status: response.status });
});

export const PATCH = authedPatch as unknown as AppRouteHandler;
export const DELETE = authedDelete as unknown as AppRouteHandler;
