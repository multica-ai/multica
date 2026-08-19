/** @type {import('next').NextConfig} */
const config = {
  reactStrictMode: true,
  // Same STANDALONE-gated `output: "standalone"` as apps/web's next.config —
  // Dockerfile.admin sets STANDALONE=true so the runtime image only needs the
  // traced .next/standalone output, not the full node_modules tree.
  ...(process.env.STANDALONE === "true" ? { output: "standalone" } : {}),
};

export default config;
