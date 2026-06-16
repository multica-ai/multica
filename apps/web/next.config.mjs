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
  // Deliberately NO outputFileTracingIncludes here. A previous
  // `"./node_modules/next/**/*"` pin (added for "Cannot find module 'next'",
  // whose real cause was excluding .next/** below) backfired badly: include
  // globs are PROJECT-relative (apps/web), so the glob resolved through the
  // apps/web/node_modules/next pnpm symlink and materialized next as a real,
  // flattened directory in the standalone output — shadowing the proper
  // symlink into node_modules/.pnpm/. From that flattened copy, Node's
  // realpath-based resolution can never reach next's deps inside .pnpm,
  // crashing the container at boot with "Cannot find module
  // '@swc/helpers/_/_interop_require_default'". Normal tracing (with .next/**
  // NOT excluded) already emits next + all its deps as a correct .pnpm
  // symlink farm. Do not re-add force-includes for packages that are
  // reachable through pnpm symlinks.
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
      // beforeFiles runs before the catch-all below. A DEDICATED (non-catch-all)
      // rewrite is required for the interactive-terminal live-stream WebSocket to
      // be upgraded by the Next.js standalone proxy: the catch-all `/api/:path*`
      // forwards HTTP fine but does NOT forward the WS `upgrade` for that path, so
      // the browser session-poll returns 200 yet the terminal WS closes 1006 and
      // the panel sits on "Waiting for agent…". Mirrors the working `/ws` rewrite.
      // (TECH-3388)
      beforeFiles: [
        {
          source: "/api/cerebro/terminal/sessions/:sid/ws",
          destination: `${remoteApiUrl}/api/cerebro/terminal/sessions/:sid/ws`,
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
