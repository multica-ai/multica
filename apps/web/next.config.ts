import type { NextConfig } from "next";
import { config } from "dotenv";
import { resolve } from "path";

// Load root .env so REMOTE_API_URL is available to next.config.ts
config({ path: resolve(__dirname, "../../.env") });

const remoteApiUrl = process.env.REMOTE_API_URL || "http://localhost:8080";
const docsUrl = process.env.DOCS_URL || "http://localhost:4000";

// Parse hostnames from CORS_ALLOWED_ORIGINS so that Next.js dev server
// allows cross-origin HMR / webpack requests (e.g. from Tailscale IPs).
const allowedDevOrigins = process.env.CORS_ALLOWED_ORIGINS
  ? process.env.CORS_ALLOWED_ORIGINS.split(",")
      .map((origin) => {
        try {
          return new URL(origin.trim()).host;
        } catch {
          return origin.trim();
        }
      })
      .filter(Boolean)
  : undefined;

const nextConfig: NextConfig = {
  ...(process.env.STANDALONE === "true" ? { output: "standalone" as const } : {}),
  transpilePackages: ["@multica/core", "@multica/ui", "@multica/views"],
  ...(allowedDevOrigins && allowedDevOrigins.length > 0
    ? { allowedDevOrigins }
    : {}),
  images: {
    formats: ["image/avif", "image/webp"],
    qualities: [75, 80, 85],
  },
  async rewrites() {
    return {
      // Run before file-system routes so /docs isn't shadowed by the
      // [workspaceSlug] dynamic segment.
      beforeFiles: [
        {
          source: "/docs",
          destination: `${docsUrl}/docs`,
        },
        {
          source: "/docs/:path*",
          destination: `${docsUrl}/docs/:path*`,
        },
      ],
      afterFiles: [
        {
          source: "/api/:path*",
          destination: `${remoteApiUrl}/api/:path*`,
        },
        {
          source: "/ws",
          destination: `${remoteApiUrl}/ws`,
        },
        {
          source: "/sse",
          destination: `${remoteApiUrl}/sse`,
        },
        {
          source: "/auth/:path*",
          destination: `${remoteApiUrl}/auth/:path*`,
        },
        {
          source: "/uploads/:path*",
          destination: `${remoteApiUrl}/uploads/:path*`,
        },
      ],
      fallback: [],
    };
  },
};

export default nextConfig;
