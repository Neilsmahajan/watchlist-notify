import type { MetadataRoute } from "next";

const SITE = "https://watchlistnotify.com";

export default function sitemap(): MetadataRoute.Sitemap {
  return [
    { url: `${SITE}/`, priority: 1 },
    { url: `${SITE}/demo`, priority: 0.9 },
    { url: `${SITE}/contact`, priority: 0.3 },
    { url: `${SITE}/privacy`, priority: 0.2 },
    { url: `${SITE}/terms`, priority: 0.2 },
  ];
}
