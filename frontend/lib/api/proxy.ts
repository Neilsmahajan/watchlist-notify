import { NextResponse } from "next/server";
import { auth0 } from "@/lib/auth0";
import { getAccessTokenOrResponse } from "@/lib/auth/api-token";
import { backendUrl } from "@/lib/api/backend";

// The browser never talks to the Go API directly: these route handlers add
// the user's access token and pass the answer back unchanged.

export type RouteHandler = (req: Request) => Promise<Response>;

type ForwardOptions = {
  method?: string;
  // A JSON body or a FormData upload.
  body?: unknown;
  timeoutMs?: number;
};

export async function forward(
  path: string,
  { method = "GET", body, timeoutMs = 25_000 }: ForwardOptions = {},
): Promise<Response> {
  const access = await getAccessTokenOrResponse();
  if (!access.ok) return access.response;

  const headers: Record<string, string> = {
    Authorization: `Bearer ${access.token}`,
    Accept: "application/json",
  };
  let payload: BodyInit | undefined;
  if (body instanceof FormData) {
    payload = body;
  } else if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }

  let response: Response;
  try {
    response = await fetch(backendUrl(path), {
      method,
      headers,
      body: payload,
      cache: "no-store",
      signal: AbortSignal.timeout(timeoutMs),
    });
  } catch (error) {
    console.error(`proxy ${method} ${path.split("?")[0]} failed`, error);
    return NextResponse.json(
      { error: "The service could not be reached. Try again in a moment." },
      { status: 502 },
    );
  }

  if (response.status === 204) {
    return new NextResponse(null, { status: 204 });
  }
  const data = await response.json().catch(() => ({}));
  // How long to wait before trying again is part of the answer.
  const retryAfter = response.headers.get("Retry-After");
  return NextResponse.json(data, {
    status: response.status,
    headers: retryAfter ? { "Retry-After": retryAfter } : undefined,
  });
}

// Wraps a handler so it only runs for a signed-in user.
export function authed(handler: RouteHandler): RouteHandler {
  return auth0.withApiAuthRequired(handler) as unknown as RouteHandler;
}

export async function readJsonObject(
  req: Request,
): Promise<Record<string, unknown> | Response> {
  let body: unknown;
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "invalid json" }, { status: 400 });
  }
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    return NextResponse.json({ error: "invalid body" }, { status: 400 });
  }
  return body as Record<string, unknown>;
}

export function lastSegment(req: Request): string {
  const segments = new URL(req.url).pathname.split("/").filter(Boolean);
  return encodeURIComponent(segments.pop() ?? "");
}
