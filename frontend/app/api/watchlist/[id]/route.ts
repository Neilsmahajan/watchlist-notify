import { NextResponse } from "next/server";
import {
  authed,
  forward,
  lastSegment,
  readJsonObject,
} from "@/lib/api/proxy";

const missingId = () =>
  NextResponse.json({ error: "id required" }, { status: 400 });

export const PATCH = authed(async (req) => {
  const id = lastSegment(req);
  if (!id) return missingId();
  const body = await readJsonObject(req);
  if (body instanceof Response) return body;
  return forward(`/api/watchlist/${id}`, { method: "PATCH", body });
});

export const DELETE = authed(async (req) => {
  const id = lastSegment(req);
  if (!id) return missingId();
  return forward(`/api/watchlist/${id}`, { method: "DELETE" });
});
