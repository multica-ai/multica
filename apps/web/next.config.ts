import type { NextConfig } from "next";
import { config } from "dotenv";
import { resolve } from "path";
import {
  resolveDevDocsUrl,
  resolveDevRemoteApiUrl,
  resolveDocsUrl,
  resolveRemoteApiUrl,
} from "./config/runtime-urls";
import { createMDX } from "fumadocs-mdx/next";

// Load root .env so local next.config.ts rewrites see REMOTE_API_URL / DOCS_URL.
// Production requests use proxy.ts runtime rewrites, which read process.env
// when the Next.js server runs instead of baking these URLs at build time.
config({ path: resolve(__dirname, "../../.env") });

// `next dev` falls back to the conventional localhost upstreams; builds use
// the strict resolvers so prebuilt images keep unset upstreams unproxied.
const isDev = process.env.NODE_ENV === "development";
const remoteApiUrl = isDev
  ? resolveDevRemoteApiUrl(process.env)
  : resolveRemoteApiUrl(process.env);
const docsUrl = isDev
  ? resolveDevDocsUrl(process.env)
  : resolveDocsUrl(process.env);

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
  transpilePackages: [
    "@multica/core",
    "@multica/ui",
    "@multica/views",
    "@multica/tutti-agent-gui-host",
    "@tutti-os/agent-activity-core",
    "@tutti-os/agent-gui",
    "@tutti-os/analytics",
    "@tutti-os/browser-node",
    "@tutti-os/client-tuttid-ts",
    "@tutti-os/connector-contracts",
    "@tutti-os/connector-renderer",
    "@tutti-os/event-protocol",
    "@tutti-os/event-stream-core",
    "@tutti-os/ui-i18n-runtime",
    "@tutti-os/ui-react-hooks",
    "@tutti-os/ui-rich-text",
    "@tutti-os/ui-system",
    "@tutti-os/workbench-snapshot",
    "@tutti-os/workbench-surface",
    "@tutti-os/workspace-external-core",
    "@tutti-os/workspace-file-manager",
    "@tutti-os/workspace-file-preview",
    "@tutti-os/workspace-file-reference",
    "@tutti-os/workspace-issue-manager",
    "@tutti-os/workspace-user-project",
  ],
  webpack(config, { isServer }) {
    // The GUI only mounts after a client effect, so server bundles leave it out.
    if (isServer) config.resolve.alias["@multica/tutti-agent-gui-host/mount"] = false;
    // The GUI's sources import images as URLs, not Next image objects.
    const vendored = resolve(process.cwd(), "../../vendor/tutti");
    const visit = (rules: unknown[]) => {
      for (const rule of rules as { loader?: string; exclude?: unknown; oneOf?: unknown[] }[]) {
        if (rule?.loader === "next-image-loader") rule.exclude = (file: string) => file.startsWith(vendored);
        if (rule?.oneOf) visit(rule.oneOf);
      }
    };
    visit(config.module.rules);
    // A function test: Next stops handling every .svg once a rule's regex matches them.
    config.module.rules.push({ test: (file: string) => /\.(png|jpe?g|gif|webp|svg|avif)$/i.test(file), include: vendored, type: "asset/resource" });
    return config;
  },
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
      beforeFiles: docsUrl
        ? [
            {
              source: "/docs",
              destination: `${docsUrl}/docs`,
            },
            {
              source: "/docs/:path*",
              destination: `${docsUrl}/docs/:path*`,
            },
          ]
        : [],
      afterFiles: remoteApiUrl
        ? [
            {
              source: "/v1/:path*",
              destination: `${remoteApiUrl}/v1/:path*`,
            },
            {
              source: "/api/:path*",
              destination: `${remoteApiUrl}/api/:path*`,
            },
            {
              source: "/ws",
              destination: `${remoteApiUrl}/ws`,
            },
            {
              source: "/health",
              destination: `${remoteApiUrl}/health`,
            },
            {
              source: "/auth/:path*",
              destination: `${remoteApiUrl}/auth/:path*`,
            },
            {
              source: "/uploads/:path*",
              destination: `${remoteApiUrl}/uploads/:path*`,
            },
          ]
        : [],
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
