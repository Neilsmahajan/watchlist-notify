import { authed, forward, readJsonObject } from "@/lib/api/proxy";

export const GET = authed(() => forward("/api/me/services"));

export const PATCH = authed(async (req) => {
  const body = await readJsonObject(req);
  if (body instanceof Response) return body;
  return forward("/api/me/services", { method: "PATCH", body });
});
