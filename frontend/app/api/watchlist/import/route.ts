import { NextResponse } from "next/server";
import { authed, forward } from "@/lib/api/proxy";

// A large export is matched against TMDB row by row.
export const maxDuration = 60;

export const POST = authed(async (req) => {
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
  return forward("/api/watchlist/import", {
    method: "POST",
    body: upload,
    timeoutMs: 58_000,
  });
});
