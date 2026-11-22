import { demoProxy } from "@/lib/api/demo-proxy";

export const GET = (req: Request) => demoProxy(req, "/api/demo/watchlist");
