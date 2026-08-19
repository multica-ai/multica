import tailwindcss from '@tailwindcss/vite';
import { tanstackStart } from '@tanstack/react-start/plugin/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';
import { fileURLToPath, URL } from 'node:url';
import { vibesTagUnifiedGateway } from './src/platform/dev-gateway-plugin';

export default defineConfig({
  base: '/tag/',
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    host: '127.0.0.1',
    port: 3100,
    strictPort: true,
    hmr: {
      path: '/tag/__vite_hmr',
    },
  },
  plugins: [
    vibesTagUnifiedGateway(),
    tailwindcss(),
    tanstackStart({
      srcDirectory: 'src',
      start: { entry: './start.tsx' },
      server: { entry: './server.ts' },
    }),
    react(),
  ],
});
