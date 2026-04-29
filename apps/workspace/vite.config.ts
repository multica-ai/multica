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
    env.VITE_API_PROXY_TARGET || env.VITE_API_URL,
    `http://localhost:${env.PORT || "8080"}`,
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
