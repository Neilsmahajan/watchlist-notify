import type { NextConfig } from "next";

// Posters and logos are plain <img> tags pointing at TMDB's CDN, which
// already serves them in the widths the pages ask for. Nothing goes through
// the image optimizer, so there are no remote image hosts to allow.
const nextConfig: NextConfig = {};

export default nextConfig;
