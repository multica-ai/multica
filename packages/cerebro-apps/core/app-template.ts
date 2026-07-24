export type AppSourceFile = {
  path: string;
  content: string;
  media_type: string;
};

export function createAppTemplate(name: string, version = "0.1.0"): AppSourceFile[] {
  const manifest = {
    manifest: {
      schema_version: "1",
      name,
      version,
      scopes: [],
      frontend: { entry: "frontend/index.html" },
      backend: { entry: "backend/index.mjs" },
    },
  };
  return [
    { path: "app.json", media_type: "application/json", content: JSON.stringify(manifest, null, 2) },
    { path: "frontend/index.html", media_type: "text/html; charset=utf-8", content: "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Mini App</title></head><body><main><h1>Mini App</h1><form><input id=\"value\" required><button>Run</button></form><pre id=\"result\"></pre></main><script type=\"module\" src=\"./app.js\" crossorigin=\"use-credentials\"></script></body></html>" },
    { path: "frontend/app.js", media_type: "text/javascript; charset=utf-8", content: "import { createMulticaApp } from '/api/cerebro/apps-runtime/sdk/multica.js';\nconst identity = window.location.pathname.match(/\\/apps-runtime\\/apps\\/([^/]+)\\/([^/]+)(?:\\/|$)/);\nif (!identity) throw new Error('Invalid app runtime URL');\nconst multica = createMulticaApp({ appId: decodeURIComponent(identity[1]), version: decodeURIComponent(identity[2]) });\ndocument.querySelector('form').addEventListener('submit', async (event) => { event.preventDefault(); const result = await multica.workers.invoke({ value: document.querySelector('#value').value }); document.querySelector('#result').textContent = JSON.stringify(result, null, 2); });" },
    { path: "backend/index.mjs", media_type: "text/javascript; charset=utf-8", content: "export default async (input) => ({ value: String(input.value ?? '') });" },
  ];
}
