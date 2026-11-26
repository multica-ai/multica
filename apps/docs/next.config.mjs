import { createMDX } from "fumadocs-mdx/next";

const withMDX = createMDX();

/** @type {import('next').NextConfig} */
const config = {
  reactStrictMode: true,
  // Otherwise `next dev` writes a generic apps/docs/AGENTS.md; agent guidance
  // lives in the root AGENTS.md.
  agentRules: false,
  basePath: "/docs",
  // Page URLs answer HTML or Markdown by `Accept` (see proxy.ts), so caches
  // must key on it. Hosts that serve prerendered pages from their CDN apply
  // this header there; when `next start` renders a page, Next replaces `Vary`
  // with its own (next/dist/build/templates/app-page-runtime.js).
  async headers() {
    return [{ source: "/((?!_next|api)[^.]*)", headers: [{ key: "Vary", value: "Accept" }] }];
  },
  // Visiting http://host/ (outside basePath) would otherwise 404 — redirect
  // to the docs root. basePath: false makes the source and destination
  // literal (not re-prefixed with `/docs`), so the redirect runs before
  // basePath routing kicks in.
  async redirects() {
    return [
      {
        source: "/",
        destination: "/docs",
        basePath: false,
        permanent: false,
      },
      {
        source: "/zh/getting-started/cloud-quickstart",
        destination: "/zh/cloud-quickstart",
        permanent: true,
      },
      {
        source: "/zh/getting-started/self-hosting",
        destination: "/zh/self-host-quickstart",
        permanent: true,
      },
      {
        source: "/zh/guides/quickstart",
        destination: "/zh/cloud-quickstart",
        permanent: true,
      },
      {
        source: "/zh/guides/agents",
        destination: "/zh/agents",
        permanent: true,
      },
      {
        source: "/zh/cli/installation",
        destination: "/zh/cli",
        permanent: true,
      },
      {
        source: "/zh/cli/reference",
        destination: "/zh/cli",
        permanent: true,
      },
    ];
  },
};

export default withMDX(config);
