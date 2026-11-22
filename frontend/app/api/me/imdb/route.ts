import { NextResponse } from "next/server";
import { authed, forward, readJsonObject } from "@/lib/api/proxy";

// Linking reads the whole IMDb list before it answers, which can take about
// twenty seconds.
export const maxDuration = 30;

export const PUT = authed(async (req) => {
  const body = await readJsonObject(req);
  if (body instanceof Response) return body;
  const source = typeof body.source === "string" ? body.source.trim() : "";
  if (!source) {
    return NextResponse.json(
      { error: "Enter your IMDb user ID, a list ID or the list's address." },
      { status: 400 },
    );
  }
  return forward("/api/me/imdb", {
    method: "PUT",
    body: { source },
    timeoutMs: 28_000,
  });
});

export const DELETE = authed(() => forward("/api/me/imdb", { method: "DELETE" }));
