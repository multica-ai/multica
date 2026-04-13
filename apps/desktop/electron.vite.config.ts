import { resolve } from "path";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import { loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const remoteApi = env.VITE_REMOTE_API;
  const remoteWs = remoteApi?.replace(/^https/, "wss").replace(/^http/, "ws");

  return {
    main: {
      plugins: [externalizeDepsPlugin()],
    },
    preload: {
      plugins: [externalizeDepsPlugin()],
    },
    renderer: {
      server: {
        port: 5173,
        strictPort: true,
        ...(remoteApi && {
          proxy: {
            "/api": { target: remoteApi, changeOrigin: true },
            "/auth": { target: remoteApi, changeOrigin: true },
            "/uploads": { target: remoteApi, changeOrigin: true },
            "/ws": { target: remoteWs, changeOrigin: true, ws: true },
          },
        }),
      },
      plugins: [react(), tailwindcss()],
      resolve: {
        alias: {
          "@": resolve("src/renderer/src"),
        },
        dedupe: ["react", "react-dom"],
      },
    },
  };
});
