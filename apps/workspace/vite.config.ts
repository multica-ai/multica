import path from "path";
import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

function envOrFallback(value: string | undefined, fallback: string): string {
  return value && value.trim() ? value : fallback;
}

export default defineConfig(({ mode }) => {
  const repoRoot = path.resolve(__dirname, "../..");
  const env = {
    ...loadEnv(mode, repoRoot, ""),
    ...loadEnv(mode, process.cwd(), ""),
    ...process.env,
  };
  const apiTarget = envOrFallback(
    env.VITE_API_PROXY_TARGET || env.VITE_API_URL || env.NEXT_PUBLIC_API_URL || env.REMOTE_API_URL,
    `http://localhost:${env.PORT || "8080"}`,
  );
  const marketingTarget = envOrFallback(
    env.MARKETING_SITE_ORIGIN,
    `http://localhost:${env.MARKETING_PORT || "3001"}`,
  );

  return {
    plugins: [react()],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
      },
    },
    server: {
      port: Number(env.FRONTEND_PORT || "3000"),
      strictPort: true,
      proxy: {
        "/api": {
          target: apiTarget,
          changeOrigin: true,
        },
        "/auth": {
          target: apiTarget,
          changeOrigin: true,
        },
        "/ws": {
          target: apiTarget,
          changeOrigin: true,
          ws: true,
        },
        "^/$": {
          target: marketingTarget,
          changeOrigin: true,
        },
        "^/about$": {
          target: marketingTarget,
          changeOrigin: true,
        },
        "^/changelog$": {
          target: marketingTarget,
          changeOrigin: true,
        },
        "^/homepage$": {
          target: marketingTarget,
          changeOrigin: true,
        },
        "^/robots\\.txt$": {
          target: marketingTarget,
          changeOrigin: true,
        },
        "^/sitemap\\.xml$": {
          target: marketingTarget,
          changeOrigin: true,
        },
        "^/_next/.*": {
          target: marketingTarget,
          changeOrigin: true,
        },
      },
    },
    build: {
      outDir: "dist",
    },
    test: {
      environment: "jsdom",
      globals: true,
      setupFiles: ["./src/test/setup.ts"],
      include: ["src/**/*.test.{ts,tsx}"],
    },
  };
});
