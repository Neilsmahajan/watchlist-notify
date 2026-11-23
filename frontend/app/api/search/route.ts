import { NextResponse } from "next/server";
import { authed, forward } from "@/lib/api/proxy";

const PASSED_ON = ["page", "include_adult", "language", "region"];

export const GET = authed(async (req) => {
  const { searchParams } = new URL(req.url);
  const query = searchParams.get("query")?.trim();
  if (!query) {
    return NextResponse.json({ error: "query required" }, { status: 400 });
  }

  const params = new URLSearchParams({
    query,
    type: searchParams.get("type") ?? "movie",
  });
  for (const name of PASSED_ON) {
    const value = searchParams.get(name);
    if (value) params.set(name, value);
  }
  return forward(`/api/search?${params}`);
});
