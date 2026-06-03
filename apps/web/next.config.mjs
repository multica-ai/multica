// @ts-check
import { config } from "dotenv";
import { dirname, resolve } from "path";
import { fileURLToPath } from "url";
import { createMDX } from "fumadocs-mdx/next";
import withSerwistInit from "@serwist/next";

const withMDX = createMDX();
const currentDir = dirname(fileURLToPath(import.meta.url));

// Service worker for the installable PWA shell. Compiled from app/sw.ts to
// public/sw.js by serwist's webpack plugin during `next build`. Disabled in
// dev so HMR isn't intercepted.
const withSerwist = withSerwistInit({
  swSrc: "app/sw.ts",
  swDest: "public/sw.js",
  disable: process.env.NODE_ENV === "development",
  reloadOnOnline: true,
});

// Load root .env so REMOTE_API_URL is available to next.config.mjs
config({ path: resolve(currentDir, "../../.env") });

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

/** @type {import("next").NextConfig} */
const nextConfig = {
  ...(process.env.STANDALONE === "true" ? { output: "standalone" } : {}),
  // Trim the "Collecting build traces" step. On the single-Mac prod runtime
  // (sara) that step repeatedly SIGTERM'd / OOM'd at the very end of an
  // otherwise-green build — it walks the dependency graph of every route and
  // is the heaviest phase of the whole build. We pin the trace root to this
  // monorepo (so it doesn't try to trace the entire workspace) and exclude
  // build-only toolchain packages that are never needed at runtime. This cuts
  // the files traced — and the memory the step holds — without changing what
  // ships. Excludes are paths relative to outputFileTracingRoot.
  outputFileTracingRoot: resolve(currentDir, "../.."),
  outputFileTracingIncludes: {
    // next is required by server.js but @vercel/nft can miss it when
    // outputFileTracingExcludes covers .next/**  (which are the tracer's
    // own starting-point files). Pinning it here guarantees it lands in
    // standalone/node_modules/ regardless of tracer behaviour.
    "*": ["./node_modules/next/**/*"],
  },
  outputFileTracingExcludes: {
    "*": [
      "node_modules/.pnpm/@swc+core*/**",
      "node_modules/.pnpm/@esbuild+*/**",
      "node_modules/.pnpm/esbuild@*/**",
      "node_modules/.pnpm/webpack@*/**",
      "node_modules/.pnpm/terser@*/**",
      "node_modules/.pnpm/@next+swc-*/**",
      "node_modules/.pnpm/typescript@*/**",
      "node_modules/.pnpm/@playwright+*/**",
      "node_modules/.pnpm/playwright*/**",
      // .next.new and .next.old are safe to exclude — they are the atomic-swap
      // side-directories from deploy.sh, never needed at runtime.
      // DO NOT exclude apps/web/.next/** — the tracer starts from compiled
      // route files in .next/server/ to discover runtime dependencies. Excluding
      // that directory causes next (and other route-only deps) to be omitted
      // from standalone/node_modules/, breaking container start-up.
      "apps/web/.next.new/**",
      "apps/web/.next.old/**",
    ],
  },
  // Allow deploy.sh to build into a side-by-side directory and atomically
  // swap it in, so the running next-server keeps serving from the previous
  // .next/ for the entire build window. Default (.next) is unchanged for dev.
  ...(process.env.NEXT_DIST_DIR ? { distDir: process.env.NEXT_DIST_DIR } : {}),
  transpilePackages: [
    "@multica/cerebro-access",
    "@multica/cerebro-agent-capabilities",
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
