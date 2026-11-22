import { authed, forward } from "@/lib/api/proxy";

export const GET = authed(() => forward("/api/me"));
