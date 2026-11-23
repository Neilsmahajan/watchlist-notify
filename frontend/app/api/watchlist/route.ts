import { NextResponse } from "next/server";
import { authed, forward, readJsonObject } from "@/lib/api/proxy";

export const GET = authed((req) => {
  const { search } = new URL(req.url);
  return forward(`/api/watchlist${search}`);
});

export const POST = authed(async (req) => {
  const body = await readJsonObject(req);
  if (body instanceof Response) return body;

  const { title, type, year, tmdb_id: tmdbId } = body;
  if (typeof title !== "string" || !title.trim()) {
    return NextResponse.json({ error: "title required" }, { status: 400 });
  }
  if (type !== "movie" && type !== "show") {
    return NextResponse.json({ error: "invalid type" }, { status: 400 });
  }

  const payload: Record<string, unknown> = { title: title.trim(), type };
  if (typeof year === "number") payload.year = year;
  if (typeof tmdbId === "number") payload.tmdb_id = tmdbId;

  return forward("/api/watchlist", { method: "POST", body: payload });
});
