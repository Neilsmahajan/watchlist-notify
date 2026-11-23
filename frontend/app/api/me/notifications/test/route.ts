import { authed, forward } from "@/lib/api/proxy";

export const POST = authed(async (req) => {
  // An empty body is fine: the API then sends the digest test.
  const body = await req.json().catch(() => ({}));
  return forward("/api/me/notifications/test", { method: "POST", body });
});
