import { NextResponse } from "next/server";
import { auth0 } from "@/lib/auth0";

type AppRouteHandler = (
  req: Request,
) => void | Response | Promise<void | Response>;

const authedPost = auth0.withApiAuthRequired(async function handler(
  req: Request,
) {
  const backend = process.env.BACKEND_URL || "http://localhost:8080";

  let incoming: FormData;
  try {
    incoming = await req.formData();
  } catch {
    return NextResponse.json(
      { error: "failed to parse upload" },
      { status: 400 },
    );
  }

  const file = incoming.get("file");
  if (!(file instanceof File)) {
    return NextResponse.json({ error: "file is required" }, { status: 400 });
  }

  const upload = new FormData();
  upload.set("file", file, file.name || "watchlist.csv");

  const { token } = await auth0.getAccessToken();

  const response = await fetch(`${backend}/api/watchlist/import`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: "application/json",
    },
    body: upload,
  });

  const data = await response.json().catch(() => ({}));
  return NextResponse.json(data, { status: response.status });
});

export const POST = authedPost as unknown as AppRouteHandler;
