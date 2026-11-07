import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // The end-to-end run builds into its own folder, so it never disturbs a running dev server.
  distDir: process.env.NEXT_DIST_DIR || ".next",
};

export default nextConfig;
