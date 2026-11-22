import { NextResponse } from "next/server";
import { authed, forward, lastSegment } from "@/lib/api/proxy";

export const GET = authed(async (req) => {
  const id = lastSegment(req);
  if (!id) {
    return NextResponse.json({ error: "id required" }, { status: 400 });
  }
  const { searchParams } = new URL(req.url);
  const type = searchParams.get("type");
  if (!type) {
    return NextResponse.json({ error: "type required" }, { status: 400 });
  }
  const params = new URLSearchParams({ type });
  const region = searchParams.get("region");
  if (region) params.set("region", region);
  return forward(`/api/availability/${id}?${params}`);
});
