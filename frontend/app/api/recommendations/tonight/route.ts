import { authed, forward, readJsonObject } from "@/lib/api/proxy";

// The API gives a pick 27 seconds at most (20 of them for the model), so this
// route waits a little longer than that and no longer.
export const maxDuration = 30;

const FIELDS = ["mood", "max_minutes", "type", "exclude"];

export const POST = authed(async (req) => {
  const body = await readJsonObject(req);
  if (body instanceof Response) return body;

  // Only the fields the API knows are passed on. It does the validating.
  const payload = Object.fromEntries(
    FIELDS.filter((field) => field in body).map((field) => [field, body[field]]),
  );
  return forward("/api/recommendations/tonight", {
    method: "POST",
    body: payload,
    timeoutMs: 29_000,
  });
});
