import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Tests and the e2e harness only. The apps bundle the GUI's sources themselves.
export default defineConfig({
  root: fileURLToPath(new URL(".", import.meta.url)),
  plugins: [react(), tailwindcss()],
  resolve: {
    dedupe: ["react", "react-dom", "@tiptap/core", "@tiptap/pm", "@tiptap/react"],
  },
});
