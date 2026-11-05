import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import { createServer } from "node:http";
import { resolve, extname } from "node:path";

const publicDir = resolve(process.argv[2] ?? ".tmp/conversation-view-browser");
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".woff2": "font/woff2" };
const server = createServer(async (request, response) => {
  const pathname = new URL(request.url, "http://localhost").pathname.replace(/^\/tutti-agent-gui\//, "/");
  const target = resolve(publicDir, `.${decodeURIComponent(pathname)}`);
  if (!target.startsWith(publicDir + "/")) { response.writeHead(403).end(); return; }
  try { response.setHeader("content-type", mime[extname(target)] ?? "application/octet-stream"); response.end(await readFile(target)); }
  catch { response.writeHead(404).end(); }
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
try {
  const child = spawn("pnpm", ["exec", "playwright", "test", "--config", "scripts/playwright-conversation-view.config.ts"], {
    stdio: "inherit",
    env: { ...process.env, MULTICA_CONVERSATION_FIXTURE_URL: `http://127.0.0.1:${server.address().port}` }
  });
  const code = await new Promise((resolve, reject) => { child.once("error", reject); child.once("exit", resolve); });
  process.exitCode = code ?? 1;
} finally {
  await new Promise((resolve) => server.close(resolve));
}
