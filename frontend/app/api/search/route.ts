import { NextResponse } from "next/server";
import { auth0 } from "@/lib/auth0";

type AppRouteHandler = (
  req: Request,
) => void | Response | Promise<void | Response>;

const authed = auth0.withApiAuthRequired(async function handler(req: Request) {
  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  const { searchParams } = new URL(req.url);

  const query = searchParams.get("query");
  const type = searchParams.get("type") ?? "movie";
  const page = searchParams.get("page");
  const includeAdult = searchParams.get("include_adult");
  const language = searchParams.get("language");
  const region = searchParams.get("region");

  if (!query || !query.trim()) {
    return NextResponse.json({ error: "query required" }, { status: 400 });
  }

  const { token } = await auth0.getAccessToken();

  const backendUrl = new URL("/api/search", backend);
  backendUrl.searchParams.set("query", query);
  backendUrl.searchParams.set("type", type);
  if (page) backendUrl.searchParams.set("page", page);
  if (includeAdult) backendUrl.searchParams.set("include_adult", includeAdult);
  if (language) backendUrl.searchParams.set("language", language);
  if (region) backendUrl.searchParams.set("region", region);

  const response = await fetch(backendUrl, {
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: "application/json",
    },
  });

  const data = await response.json().catch(() => ({}));
  return NextResponse.json(data, { status: response.status });
});

export const GET = authed as unknown as AppRouteHandler;
