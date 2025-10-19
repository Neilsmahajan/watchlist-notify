import { NextResponse } from "next/server";
import { auth0 } from "../auth0";

type AccessTokenSuccess = {
  ok: true;
  token: string;
};

type AccessTokenFailure = {
  ok: false;
  response: Response;
};

const WWW_AUTHENTICATE_HEADER =
  'Bearer error="invalid_token", error_description="Session expired"';

const sessionExpiredResponse = (code = "session_expired"): Response =>
  NextResponse.json(
    {
      error: "Your session has expired. Please sign in again.",
      code: "session_expired",
    },
    {
      status: 401,
      headers: {
        "WWW-Authenticate": WWW_AUTHENTICATE_HEADER,
        "X-Auth-Error": code,
        "Cache-Control": "no-store",
      },
    },
  );

const genericFailureResponse = (): Response =>
  NextResponse.json(
    { error: "Unable to authenticate request." },
    {
      status: 500,
      headers: {
        "Cache-Control": "no-store",
      },
    },
  );

const isAccessTokenError = (value: unknown): value is { code?: string } => {
  if (!value || typeof value !== "object") {
    return false;
  }
  const maybeError = value as { name?: unknown };
  return maybeError.name === "AccessTokenError";
};

export async function getAccessTokenOrResponse(): Promise<
  AccessTokenSuccess | AccessTokenFailure
> {
  try {
    const { token } = await auth0.getAccessToken();
    if (!token) {
      return { ok: false, response: sessionExpiredResponse() };
    }
    return { ok: true, token };
  } catch (error) {
    if (isAccessTokenError(error)) {
      const code = typeof error.code === "string" ? error.code : undefined;
      return {
        ok: false,
        response: sessionExpiredResponse(code ?? "access_token_error"),
      };
    }
    console.error("Access token retrieval failed", error);
    return { ok: false, response: genericFailureResponse() };
  }
}
