import type { NextConfig } from "next";
import { config } from "dotenv";
import { resolve } from "path";

// Load root .env so REMOTE_API_URL is available to next.config.ts
config({ path: resolve(__dirname, "../../.env") });

const remoteApiUrl = process.env.REMOTE_API_URL || "http://localhost:8080";
const distDir = process.env.NEXT_DIST_DIR?.trim() || ".next";

const nextConfig: NextConfig = {
  distDir,
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
