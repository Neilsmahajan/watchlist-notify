import type { MetadataRoute } from "next";

export default function robots(): MetadataRoute.Robots {
  return {
    rules: {
      userAgent: "*",
      allow: "/",
      // Pages behind the login, and the routes that serve them.
      disallow: ["/api/", "/auth/", "/watchlist", "/search", "/settings", "/profile"],
    },
    sitemap: "https://watchlistnotify.com/sitemap.xml",
  };
}
