import type { NextConfig } from "next";

// In local dev, dotenv loads root .env so REMOTE_API_URL is available.
// In Docker, env vars are passed directly — dotenv is not needed.
try {
  const { config } = require("dotenv");
  const { resolve } = require("path");
  config({ path: resolve(__dirname, "../../.env") });
} catch (e: unknown) {
  if (e instanceof Error && "code" in e && (e as NodeJS.ErrnoException).code !== "MODULE_NOT_FOUND") throw e;
  // dotenv not available (e.g. Docker build) — env vars come from the environment.
}

const remoteApiUrl =
  process.env.REMOTE_API_URL || process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

const nextConfig: NextConfig = {
  output: "standalone",
  images: {
    formats: ["image/avif", "image/webp"],
    qualities: [75, 80, 85],
  },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${remoteApiUrl}/api/:path*`,
      },
      {
        source: "/ws",
        destination: `${remoteApiUrl}/ws`,
      },
      {
        source: "/auth/:path*",
        destination: `${remoteApiUrl}/auth/:path*`,
      },
    ];
  },
};

export default nextConfig;
