import type { NextConfig } from "next";
import { config } from "dotenv";
import { resolve } from "path";
import { resolveRemoteApiUrl } from "./config/runtime-urls";
import { createMDX } from "fumadocs-mdx/next";

// Load root .env so REMOTE_API_URL is available to next.config.ts
config({ path: resolve(__dirname, "../../.env") });

const remoteApiUrl = resolveRemoteApiUrl(process.env);
const docsUrl = process.env.DOCS_URL || "http://localhost:4000";

// Web build/deploy version exposed to the browser bundle as
// NEXT_PUBLIC_APP_VERSION (read in components/web-providers.tsx and reported to
// client_usage_daily.client_version). Priority:
//   1. explicit NEXT_PUBLIC_APP_VERSION — release workflow / manual override
//   2. VERCEL_DEPLOYMENT_ID — unique per Vercel deployment
//   3. VERCEL_GIT_COMMIT_SHA — the deployed commit
// When none is set (local dev, plain Docker build) we leave it unset so
// web-providers falls back to the package.json version, preserving today's
// behavior for the release/Docker paths that already pass the value explicitly.
// Requires "Automatically expose System Environment Variables" enabled on the
// Vercel project so VERCEL_* are available at build time.
const webVersion =
  process.env.NEXT_PUBLIC_APP_VERSION ||
  process.env.VERCEL_DEPLOYMENT_ID ||
  process.env.VERCEL_GIT_COMMIT_SHA;

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
  ...(webVersion ? { env: { NEXT_PUBLIC_APP_VERSION: webVersion } } : {}),
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

// fumadocs-mdx@12 is incompatible with Next 16's Turbopack: its loader fails to
// dynamic-import `.source/source.config.mjs` under the Turbopack Node evaluator
// (see fumadocs#2658). `dev`/`build` scripts pass `--webpack` to opt out.
// Drop the flag once fumadocs-mdx ships a Turbopack-compatible loader.
const withMDX = createMDX() as (config: NextConfig) => NextConfig;

export default withMDX(nextConfig);
