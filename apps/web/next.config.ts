import type { NextConfig } from "next";
import { config } from "dotenv";
import { resolve } from "path";
import { createMDX } from "fumadocs-mdx/next";
import withSerwistInit from "@serwist/next";

const withMDX = createMDX();

// Service worker for the installable PWA shell. Compiled from app/sw.ts to
// public/sw.js by serwist's webpack plugin during `next build`. Disabled in
// dev so HMR isn't intercepted.
const withSerwist = withSerwistInit({
  swSrc: "app/sw.ts",
  swDest: "public/sw.js",
  disable: process.env.NODE_ENV === "development",
  reloadOnOnline: true,
});

// Load root .env so REMOTE_API_URL is available to next.config.ts
config({ path: resolve(__dirname, "../../.env") });

const remoteApiUrl = process.env.REMOTE_API_URL || process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

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
  // Allow deploy.sh to build into a side-by-side directory and atomically
  // swap it in, so the running next-server keeps serving from the previous
  // .next/ for the entire build window. Default (.next) is unchanged for dev.
  ...(process.env.NEXT_DIST_DIR ? { distDir: process.env.NEXT_DIST_DIR } : {}),
  transpilePackages: [
    "@multica/cerebro-access",
    "@multica/cerebro-artifacts",
    "@multica/cerebro-attachments",
    "@multica/cerebro-budgets",
    "@multica/cerebro-channels",
    "@multica/cerebro-chat",
    "@multica/cerebro-credentials",
    "@multica/cerebro-dashboard",
    "@multica/cerebro-dictation",
    "@multica/cerebro-feature-flags",
    "@multica/cerebro-groups",
    "@multica/cerebro-inbox",
    "@multica/cerebro-members",
    "@multica/cerebro-notifications",
    "@multica/cerebro-permissions",
    "@multica/cerebro-pin-input",
    "@multica/cerebro-preferences",
    "@multica/cerebro-profile",
    "@multica/cerebro-realtime",
    "@multica/cerebro-runtime",
    "@multica/cerebro-skill-mention",
    "@multica/cerebro-tasks",
    "@multica/cerebro-types",
    "@multica/cerebro-ui",
    "@multica/cerebro-users",
    "@multica/cerebro-workflows",
    "@multica/core",
    "@multica/ui",
    "@multica/views",
  ],
  ...(allowedDevOrigins && allowedDevOrigins.length > 0
    ? { allowedDevOrigins }
    : {}),
  images: {
    formats: ["image/avif", "image/webp"],
    qualities: [75, 80, 85],
  },
  async rewrites() {
    return {
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
          source: "/auth/:path*",
          destination: `${remoteApiUrl}/auth/:path*`,
        },
        {
          source: "/uploads/:path*",
          destination: `${remoteApiUrl}/uploads/:path*`,
        },
        {
          source: "/install-runtime.sh",
          destination: `${remoteApiUrl}/install-runtime.sh`,
        },
      ],
      fallback: [],
    };
  },
};

export default withSerwist(withMDX(nextConfig));
