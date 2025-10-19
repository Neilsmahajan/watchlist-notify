import { NextResponse } from "next/server";
import { auth0 } from "@/lib/auth0";
import { getAccessTokenOrResponse } from "@/lib/auth/api-token";

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

const authedGet = auth0.withApiAuthRequired(async function handler(
  req: Request,
) {
  const id = getIdFromRequest(req);
  if (!id) {
    return NextResponse.json({ error: "id required" }, { status: 400 });
  }

  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  const { searchParams } = new URL(req.url);

  const type = searchParams.get("type");
  if (!type) {
    return NextResponse.json({ error: "type required" }, { status: 400 });
  }
  const region = searchParams.get("region");

  const access = await getAccessTokenOrResponse();
  if (!access.ok) {
    return access.response;
  }
  const { token } = access;

  const backendUrl = new URL(`/api/availability/${id}`, backend);
  backendUrl.searchParams.set("type", type);
  if (region) {
    backendUrl.searchParams.set("region", region);
  }

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
