import type { NextConfig } from "next";
import { config } from "dotenv";
import { resolve } from "path";

// Load root .env so REMOTE_API_URL is available to next.config.ts
config({ path: resolve(__dirname, "../../.env") });

const remoteApiUrl = process.env.REMOTE_API_URL || "http://localhost:8080";
const docsUrl = process.env.DOCS_URL || "http://localhost:4000";
const isProductionBuild = process.env.NODE_ENV === "production";
const workspaceDistAliases = {
  "@multica/core": resolve(__dirname, "../../packages/core/dist"),
  "@multica/ui": resolve(__dirname, "../../packages/ui/dist"),
  "@multica/views": resolve(__dirname, "../../packages/views/dist"),
};
const workspaceTurbopackDistAliases = {
  "@multica/core": "../../packages/core/dist",
  "@multica/ui": "../../packages/ui/dist",
  "@multica/views": "../../packages/views/dist",
};

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
  ...(isProductionBuild
    ? {}
    : { transpilePackages: ["@multica/core", "@multica/ui", "@multica/views"] }),
  ...(allowedDevOrigins && allowedDevOrigins.length > 0
    ? { allowedDevOrigins }
    : {}),
  experimental: {
    optimizePackageImports: ["@multica/views", "@multica/ui", "lucide-react"],
    cpus: 1,
    memoryBasedWorkersCount: false,
    staticGenerationMaxConcurrency: 1,
    staticGenerationMinPagesPerWorker: 1000,
    parallelServerCompiles: false,
    parallelServerBuildTraces: false,
    webpackBuildWorker: true,
    webpackMemoryOptimizations: true,
    serverSourceMaps: false,
  },
  productionBrowserSourceMaps: false,
  typescript: {
    ignoreBuildErrors: true,
  },
  images: {
    formats: ["image/avif", "image/webp"],
    qualities: [75, 80, 85],
  },
  turbopack: {
    resolveAlias: isProductionBuild ? workspaceTurbopackDistAliases : {},
  },
  webpack(config) {
    if (isProductionBuild) {
      config.resolve = config.resolve ?? {};
      config.resolve.alias = {
        ...(config.resolve.alias ?? {}),
        ...workspaceDistAliases,
      };
    }

    return config;
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

export default nextConfig;
