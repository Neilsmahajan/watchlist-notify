import { NextResponse } from "next/server";
import { auth0 } from "@/lib/auth0";

type AppRouteHandler = (
  req: Request,
) => void | Response | Promise<void | Response>;

const authedGet = auth0.withApiAuthRequired(async function handler(
  req: Request,
) {
  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  const { searchParams } = new URL(req.url);

  const backendUrl = new URL("/api/watchlist", backend);
  searchParams.forEach((value, key) => {
    backendUrl.searchParams.append(key, value);
  });

  const { token } = await auth0.getAccessToken();

  const response = await fetch(backendUrl, {
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: "application/json",
    },
  });

  const data = await response.json().catch(() => ({}));
  return NextResponse.json(data, { status: response.status });
});

export const GET = authedGet as unknown as AppRouteHandler;

const authedPost = auth0.withApiAuthRequired(async function handler(
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

  const {
    title,
    type,
    year,
    tmdb_id: tmdbId,
  } = body as {
    title?: unknown;
    type?: unknown;
    year?: unknown;
    tmdb_id?: unknown;
  };

  if (typeof title !== "string" || !title.trim()) {
    return NextResponse.json({ error: "title required" }, { status: 400 });
  }

  if (type !== "movie" && type !== "show") {
    return NextResponse.json({ error: "invalid type" }, { status: 400 });
  }

  const payload: Record<string, unknown> = {
    title: title.trim(),
    type,
  };

  if (typeof year === "number") {
    payload.year = year;
  }

  if (typeof tmdbId === "number") {
    payload.tmdb_id = tmdbId;
  }

  const { token } = await auth0.getAccessToken();

  const response = await fetch(`${backend}/api/watchlist`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify(payload),
  });

  const data = await response.json().catch(() => ({}));
  return NextResponse.json(data, { status: response.status });
});

export const POST = authedPost as unknown as AppRouteHandler;
